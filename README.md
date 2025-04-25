# WMSE Downloader

A simple tool to download MP3 archives from WMSE radio shows, including their playlists.

## Features

- Downloads MP3 archives from WMSE shows
- Automatically skips files you've already downloaded
- Downloads and saves playlists as text files
- Shows real-time download progress with a progress bar
- Displays download speed and ETA
- Respects server limits with built-in delays
- Shows download progress
- Retries failed downloads automatically
- Optional debug logging for troubleshooting

## Installation

### Pre-built Binaries

The easiest way to install WMSE Downloader is to download a pre-built binary from the [releases page](https://github.com/pdfinn/wmse_downloader/releases). We provide binaries for:

- Windows (64-bit)
- macOS (Intel and Apple Silicon)
- Linux (64-bit, ARM64)

Simply download the appropriate archive for your system, extract it, and run the executable.

### Building from Source

If you prefer to build from source:

1. Make sure you have [Go](https://golang.org/dl/) installed (version 1.21 or later)
2. Clone this repository:
   ```bash
   git clone https://github.com/pdfinn/wmse_downloader.git
   cd wmse_downloader
   ```
3. Build the program:
   ```bash
   go build
   ```

## Usage

### Basic Usage

To download archives for a show, run:
```bash
./wmse_downloader -show SHOW_ID
```

For example, to download the "Ded" show:
```bash
./wmse_downloader -show ded
```

### Command Line Options

- `-show`: The ID of the WMSE show to download (required)
- `-out`: Directory to save MP3 files (default: "./archives")
- `-delay`: Delay between downloads in seconds (default: 5)
- `-debug`: Enable detailed debug logging (default: false)
- `-version`: Show version information

Example with all options:
```bash
./wmse_downloader -show ded -out ~/Music/WMSE -delay 10 -debug
```

### Finding Show IDs

1. Visit [WMSE's website](https://wmse.org)
2. Navigate to the show you want to download
3. Look at the URL - the show ID is usually the last part of the URL
   - Example: For `https://wmse.org/program/ded/`, the show ID is `ded`

### Output

The program will:
1. Create a directory for the archives (default: `./archives`)
2. Download MP3 files with names like `2024-03-15_ded.mp3`
3. If available, create playlist files with names like `2024-03-15_ded.txt`

## Troubleshooting

- **No files downloaded**: Make sure you're using the correct show ID
- **Download errors**: Try increasing the delay between downloads
- **Missing playlists**: Not all shows have playlists available

## Security

- The program validates all inputs to prevent security issues
- Downloads are limited to 500MB per file
- Files are downloaded to a temporary location first, then moved to the final location
- All files are saved with secure permissions (readable by owner only)

## Contributing

Feel free to submit issues or pull requests!

## License

This project is licensed under the MIT License - see the LICENSE file for details. 