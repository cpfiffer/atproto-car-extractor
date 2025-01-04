ATProto CAR Extractor
=====================

This is a small helper utility to download atproto repositories as CAR files, unpack contents to JSON files, and process multiple repositories in batch.

Adapted from https://github.com/bluesky-social/cookbook/tree/main/go-repo-export

## Install

You need the Go programming language toolchain installed: <https://go.dev/doc/install>

You can directly install and run the command (without a git checkout):

```shell
go install github.com/cpfiffer/atproto-car-extractor@latest
atproto-car-extractor dids.txt
```

Or you can clone this repository and build locally:

```shell
git clone https://github.com/cpfiffer/atproto-car-extractor
cd atproto-car-extractor
go build ./...
./atproto-car-extractor dids.txt
```

## Usage

Process multiple repositories by providing a file containing DIDs (one per line):

```shell
# Using command line argument
atproto-car-extractor dids.txt

# Or using environment variable
DIDS_FILE=./dids.txt DOWNLOAD_BLOBS=true atproto-car-extractor
```

The program will:
1. Create directories for CAR files and records
2. Download each repository
3. Unpack records to JSON files
4. Optionally download blobs if DOWNLOAD_BLOBS=true

## Example

```shell
# Create a file with DIDs
# cameron.pfiffer.org did
echo "did:plc:gfrmhdmjvxn2sjedzboeudef" > dids.txt
# atproto.com did
echo "did:plc:ewvi7nxzyoun6zhxrhs64oiz)" >> dids.txt

# Process all repositories
./atproto-car-extractor dids.txt

# Process all repositories including blobs
DOWNLOAD_BLOBS=true ./atproto-car-extractor dids.txt
```

## Output Structure

```
.
├── cars/                    # CAR files for each repository
│   ├── did:plc:example1.car
│   └── did:plc:example2.car
└── records/                 # Unpacked JSON records
    ├── did:plc:example1/
    │   ├── _commit.json
    │   ├── app.bsky.actor.profile/
    │   └── _blob/          # If DOWNLOAD_BLOBS=true
    └── did:plc:example2/
        ├── _commit.json
        ├── app.bsky.feed.post/
        └── _blob/          # If DOWNLOAD_BLOBS=true
```
