Photobak
========

*UNDER CONSTRUCTION*

Photobak is a media archiver. It downloads photos and videos from cloud services like Google Photos so you have a local copy of your content in case you ever lose access to your online accounts.

Features:

- Organizes photos on disk by album
- Doesn't store duplicates
- Supports multiple accounts per service
- Runs on a schedule
- Idempotent operations
- Separate additive and destructive commands

Supported cloud services:

- Google Photos

More providers can be [added by implementing some interfaces](https://github.com/mholt/photobak/wiki/Writing-a-Cloud-Provider-Client). (Please submit a pull request if you've implemented one!)

Be sure to read the caveats below for your cloud services of choice.

## Install

Since photobak is a Go program, its binaries are static and are available for every platform. Simply [download the latest](https://github.com/mholt/photobak/releases/latest) and plop it in your PATH.

Or, to install from source:

```bash
$ go get github.com/mholt/photobak/cmd/photobak
```

## Usage

To store everything in a Google Photos account, get an OAuth2 Client ID from the [Google Developer Console](https://console.developers.google.com), then:

```bash
$ export GOOGLEPHOTOS_CLIENT_ID=...
$ export GOOGLEPHOTOS_CLIENT_SECRET=...
$ photobak -googlephotos you@yours.com
```

The first time using this account, you will be redirected to a web page where you'll authorize photobak to access your photos. Subsequent runs use the previously-stored credentials, so you won't be prompted again. However, you must continue to make your API key available in environment variables. Those aren't stored in case your database gets stolen.

To specify more accounts, just rinse and repeat:

```bash
$ photobak -googlephotos you@yours.com -googlephotos them@theirs.com
```

Photobak stores all content in a repository. The default repository is "./photos_backup", relative to the current working directory. You can change this with the `-repo` flag: `-repo ~/backups`. Inside the repository, a `.db` file is created. This is Photobak's index. Don't delete it. Don't change or move the files in the repository, or Photobak will probably try to re-download them next time. It keeps an accounting of all files in the repository.

By default, photobak only stores what it needs to do its archiving functions. You can tell it to store everything the cloud service returns with the `-everything` flag, but be aware it will increase the size of the index. For Google Photos, this extra information is things like links to thumbnails of various sizes, whether comments are enabled, license details, etc.

Photobak will not delete or move photos around once they are downloaded. If you delete a photo remotely, Photobak will not automatically delete them locally. This is because photobak is a backup utility, not a sync tool. You can force a sync by specifying the `-sync` flag, which will first delete all photos and albums that don't exist remotely anymore, and then it will continue to perform a backup.

A photo or video may appear in more than one album. This is fine, but Photobak will not store more than one copy of a photo or video. Instead, it will write the path to where the file can be found out to a file in the album called "others.txt". You can follow those paths to find the rest of the photos for an album.

## Run on a Schedule

Photobak can run indefinitely and perform its backup operations on a regular schedule with the `-every` flag:

```bash
$ photobak -every 1d
```

This will run the command every 1 day. Valid units are `m`, `h`, `d` for minute, hour, and day, respectively. You should run this in the background since it will block forever.

## Logging

TODO.

## Caveats

This program is designed to work with various cloud providers in a generic way, and each one has little things to be aware of.

### Google Photos

- There is no Google Photos API; it uses a zombied version of the [Picasa Web Albums API](https://developers.google.com/picasa-web/docs/2.0/developers_guide_protocol) which is somewhat deprecated. But it still works for now, and one advantage is that you don't have to mirror your Google Photos in Google Drive for this program to work.

- Some users [have reported](https://code.google.com/p/gdata-issues/issues/detail?id=7004) that a [maximum of 10,000 photos can be downloaded](https://github.com/camlistore/camlistore/issues/874) per album. It is still unclear why this is; even Google employees are hitting this. Google Photos puts all your "instant upload" (auto backup) photos into a single album called "Auto Backup". So if you take most of your photos on your phone and they get uploaded to Google Photos, you may hit this limit and there is no way to get photos older than the most recent 10k unless you put them into albums you create.

- Photos are retrieved approximately in order from the most recent to the oldest as they appear in your photo stream. Downloads will happen concurrently in several threads to speed things up.

