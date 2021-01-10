# `msync`

Maintain a lower-bitrate copy of your music library, in sync with the main copy. Think of it as `rsync`, but only for music files, and with the ability to transcode high-bitrate or lossless files to lower-bitrate files suitable for use on devices with less storage.

It's also more opinionated than `rsync`, with behaviors tailored for exactly this use case. For example, `msync` will always remove files from the destination which are missing from the source; whereas rsync won't do this unless you pass it the `--delete` flag.

## Use Cases

I use msync to keep a secondary, 160 Kbps AAC copy of my music library. This is synchronized occasionally to my iPhone, which lacks the space for a music library consisting of lossless and higher-bitrate files.

I accomplish this by adding a launchd job for msync, which runs nightly on the Mac Mini server whic hosts the music library.

## Installation

**Requirements:** at the moment, `msync` supports only macOS. This is because it uses macOS's built-in `afinfo` tool to determine music files' bitrates.

`make install` will build `msync` for your current OS/architecture and install it to `/usr/local/bin`.

## Usage

Basic usage is `msync -from ~/Music -to ~/MusicSmaller -max-kbps 192`, but you should review the available options as you'll likely want to use some of them.

### Options

- `-ask-trash-permission`: Trigger the macOS permission dialog for removing files immediately when the sync process begins (instead of later in the process, when we actually start removing files).
- `-dry-run`: Don't actually modify anything on the filesystem, but print what would happen, including an estimate of the final size of the destination music library.
- `-file-mode`: Octal value specifying mode for copied music files. Must begin with '0' or '0o'.
- `-from`: Path of the source music library.
- `-max-kbps`: Maximum bitrate, in Kbps, for the destination music library. Any music files of higher quality will be transcoded from the source library to the destination at this bitrate.
- `-remove-nonmusic-from-dest`: Remove any non-music files from the destination, even if they are present in the source directory tree.
- `-symlink`: For music files which are already under the maximum bitrate, create symlinks instead of actual copies. This is useful if you're mirroring your music library somewhere on the same machine, rather than directly to a portable device.
- `-to`: Path of the destination music library.
- `-verbose`: Log detailed output to stderr. Suppresses fancy progress indicators.
- `-version`: Print version and exit.

## About

- Issues: https://github.com/cdzombak/msync/issues
- Author: [Chris Dzombak](https://www.dzombak.com)
