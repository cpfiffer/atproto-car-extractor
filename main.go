package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	_ "github.com/bluesky-social/indigo/api/bsky"
	_ "github.com/bluesky-social/indigo/api/chat"
	_ "github.com/bluesky-social/indigo/api/ozone"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/ipfs/go-cid"
)

type Config struct {
    DownloadBlobs bool
    CarsDir       string
    RecordsDir    string
    DIDsFile      string
}

func ensureDirectories(config Config) error {
	dirs := []string{config.CarsDir, config.RecordsDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return nil
}

func main() {
	config := Config{
		DownloadBlobs: os.Getenv("DOWNLOAD_BLOBS") == "true",
		CarsDir:      "cars",
		RecordsDir:   "records",
		DIDsFile:     "",
	}

	// Check command line args first
	if len(os.Args) > 1 {
		config.DIDsFile = os.Args[1]
	} else {
		config.DIDsFile = os.Getenv("DIDS_FILE")
	}

	if config.DIDsFile == "" {
		fmt.Fprintf(os.Stderr, "error: Please provide DIDs file path as argument or set DIDS_FILE environment variable\n")
		fmt.Fprintf(os.Stderr, "usage: %s <dids-file>\n", os.Args[0])
		os.Exit(1)
	}

	if err := run(config); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
	}
}

func run(config Config) error {
	if err := ensureDirectories(config); err != nil {
		return err
	}

	ctx := context.Background()
	dids, err := getActivatedDIDs(ctx, config.DIDsFile)
	if err != nil {
		return fmt.Errorf("failed to get DIDs from file: %w", err)
	}

	for _, did := range dids {
		if err := processRepo(did, config); err != nil {
			fmt.Fprintf(os.Stderr, "error processing %s: %v\n", did, err)
			continue
		}
	}

	return nil
}

func processRepo(did string, config Config) error {
	ctx := context.Background()
	
	// Parse DID
	atid, err := syntax.ParseAtIdentifier(did)
	if err != nil {
		return err
	}

	// Look up the DID and PDS
	fmt.Printf("Processing: %s\n", atid.String())
	dir := identity.DefaultDirectory()
	ident, err := dir.Lookup(ctx, *atid)
	if err != nil {
		return err
	}

	// Download repo
	carPath := filepath.Join(config.CarsDir, ident.DID.String()+".car")
	if err := downloadRepo(ctx, ident, carPath); err != nil {
		return err
	}

	// Unpack records
	recordsPath := filepath.Join(config.RecordsDir, ident.DID.String())
	if err := unpackRecords(ctx, carPath, recordsPath); err != nil {
		return err
	}

	// Handle blobs if enabled
	if config.DownloadBlobs {
		if err := downloadBlobs(ctx, ident, recordsPath); err != nil {
			return err
		}
	}

	return nil
}

func downloadRepo(ctx context.Context, ident *identity.Identity, carPath string) error {
	xrpcc := xrpc.Client{
		Host: ident.PDSEndpoint(),
	}
	if xrpcc.Host == "" {
		return fmt.Errorf("no PDS endpoint for identity")
	}

	fmt.Printf("Downloading from %s to: %s\n", xrpcc.Host, carPath)
	repoBytes, err := comatproto.SyncGetRepo(ctx, &xrpcc, ident.DID.String(), "")
	if err != nil {
		return err
	}
	return os.WriteFile(carPath, repoBytes, 0666)
}

func unpackRecords(ctx context.Context, carPath, recordsPath string) error {
	fi, err := os.Open(carPath)
	if err != nil {
		return err
	}

	r, err := repo.ReadRepoFromCar(ctx, fi)
	if err != nil {
		return err
	}

	// Get commit object
	sc := r.SignedCommit()
	fmt.Printf("writing output to: %s\n", recordsPath)

	// first the commit object as a meta file
	commitPath := filepath.Join(recordsPath, "_commit")
	os.MkdirAll(filepath.Dir(commitPath), os.ModePerm)
	recJson, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(commitPath+".json", recJson, 0666); err != nil {
		return err
	}

	// then all the actual records
	err = r.ForEach(ctx, "", func(k string, v cid.Cid) error {
		_, rec, err := r.GetRecord(ctx, k)
		if err != nil {
			fmt.Printf("Warning: Failed to get record %s: %v\n", k, err)
			return nil
		}

		recPath := filepath.Join(recordsPath, k)
		fmt.Printf("%s.json\n", recPath)
		os.MkdirAll(filepath.Dir(recPath), os.ModePerm)
		recJson, err := json.MarshalIndent(rec, "", "  ")
		if err != nil {
			fmt.Printf("Warning: Failed to marshal record %s: %v\n", k, err)
			return nil
		}
		if err := os.WriteFile(recPath+".json", recJson, 0666); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func downloadBlobs(ctx context.Context, ident *identity.Identity, recordsPath string) error {
	topDir := filepath.Join(recordsPath, "_blob")
	fmt.Printf("writing blobs to: %s\n", topDir)
	os.MkdirAll(topDir, os.ModePerm)

	xrpcc := xrpc.Client{
		Host: ident.PDSEndpoint(),
	}
	if xrpcc.Host == "" {
		return fmt.Errorf("no PDS endpoint for identity")
	}

	cursor := ""
	for {
		resp, err := comatproto.SyncListBlobs(ctx, &xrpcc, cursor, ident.DID.String(), 500, "")
		if err != nil {
			return err
		}
		for _, cidStr := range resp.Cids {
			blobPath := filepath.Join(topDir, cidStr)
			if _, err := os.Stat(blobPath); err == nil {
				fmt.Printf("%s\texists\n", blobPath)
				continue
			}
			blobBytes, err := comatproto.SyncGetBlob(ctx, &xrpcc, cidStr, ident.DID.String())
			if err != nil {
				return err
			}
			if err := os.WriteFile(blobPath, blobBytes, 0666); err != nil {
				return err
			}
			fmt.Printf("%s\tdownloaded\n", blobPath)
		}
		if resp.Cursor != nil && *resp.Cursor != "" {
			cursor = *resp.Cursor
		} else {
			break
		}
	}
	return nil
}

func carUnpack(carPath string) error {
	ctx := context.Background()
	fi, err := os.Open(carPath)
	if err != nil {
		return err
	}

	r, err := repo.ReadRepoFromCar(ctx, fi)
	if err != nil {
		return err
	}

	// extract DID from repo commit
	sc := r.SignedCommit()
	did, err := syntax.ParseDID(sc.Did)
	if err != nil {
		return err
	}

	topDir := did.String()
	fmt.Printf("writing output to: %s\n", topDir)

	// first the commit object as a meta file
	commitPath := topDir + "/_commit"
	os.MkdirAll(filepath.Dir(commitPath), os.ModePerm)
	recJson, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(commitPath+".json", recJson, 0666); err != nil {
		return err
	}

	// then all the actual records
	err = r.ForEach(ctx, "", func(k string, v cid.Cid) error {
		_, rec, err := r.GetRecord(ctx, k)
		if err != nil {
			return err
		}

		recPath := topDir + "/" + k
		fmt.Printf("%s.json\n", recPath)
		os.MkdirAll(filepath.Dir(recPath), os.ModePerm)
		if err != nil {
			return err
		}
		recJson, err := json.MarshalIndent(rec, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(recPath+".json", recJson, 0666); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func blobDownloadAll(raw string) error {
	ctx := context.Background()
	atid, err := syntax.ParseAtIdentifier(raw)
	if err != nil {
		return err
	}

	// first look up the DID and PDS for this repo
	dir := identity.DefaultDirectory()
	ident, err := dir.Lookup(ctx, *atid)
	if err != nil {
		return err
	}

	// create a new API client to connect to the account's PDS
	xrpcc := xrpc.Client{
		Host: ident.PDSEndpoint(),
	}
	if xrpcc.Host == "" {
		return fmt.Errorf("no PDS endpoint for identity")
	}

	topDir := ident.DID.String() + "/_blob"
	fmt.Printf("writing blobs to: %s\n", topDir)
	os.MkdirAll(topDir, os.ModePerm)

	cursor := ""
	for {
		resp, err := comatproto.SyncListBlobs(ctx, &xrpcc, cursor, ident.DID.String(), 500, "")
		if err != nil {
			return err
		}
		for _, cidStr := range resp.Cids {
			blobPath := topDir + "/" + cidStr
			if _, err := os.Stat(blobPath); err == nil {
				fmt.Printf("%s\texists\n", blobPath)
				continue
			}
			blobBytes, err := comatproto.SyncGetBlob(ctx, &xrpcc, cidStr, ident.DID.String())
			if err != nil {
				return err
			}
			if err := os.WriteFile(blobPath, blobBytes, 0666); err != nil {
				return err
			}
			fmt.Printf("%s\tdownloaded\n", blobPath)
		}
		if resp.Cursor != nil && *resp.Cursor != "" {
			cursor = *resp.Cursor
		} else {
			break
		}
	}
	return nil
}

func readDIDsFromFile(filename string) ([]string, error) {
    content, err := os.ReadFile(filename)
    if err != nil {
        return nil, fmt.Errorf("failed to read file: %w", err)
    }

    var dids []string
    for _, line := range strings.Split(string(content), "\n") {
        line = strings.TrimSpace(line)
        if line != "" {
            dids = append(dids, line)
        }
    }

    return dids, nil
}

func getActivatedDIDs(ctx context.Context, filename string) ([]string, error) {
    return readDIDsFromFile(filename)
}