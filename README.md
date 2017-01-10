Photobak
========

Photobak is a media archiver. It downloads your photos and videos from cloud services like Google Photos so you have a local copy of your content. Run it on a regular basis to make sure you have all your new memories.

Features:

- Integrity checks
- Doesn't store duplicates
- Organizes photos on disk by album
- Fast downloads in parallel
- Can run on a schedule
- Supports multiple accounts per service
- Idempotent operations
- Separate additive and destructive commands

Supported cloud services:

- Google Photos

More providers can be [added by implementing some interfaces](https://github.com/mholt/photobak/wiki/Writing-a-Client-Implementation). (Please submit a pull request if you've implemented one!)

Be sure to read the caveats below for your cloud services of choice.

## Install

Since photobak is a Go program, its binaries are static and are available for every platform. Simply [download the latest](https://github.com/mholt/photobak/releases/latest) and plop it in your PATH.

Or, to install from source:

```bash
$ go get github.com/mholt/photobak/cmd/photobak
```

For help:

```bash
$ photobak -help
```

## Usage

To store everything in a Google Photos account, get an OAuth2 Client ID from the [Google Developer Console](https://console.developers.google.com), then:

```bash
$ export GOOGLEPHOTOS_CLIENT_ID=...
$ export GOOGLEPHOTOS_CLIENT_SECRET=...
$ photobak -googlephotos you@yours.com
```

The first time using this account, you will be redirected to a web page where you'll authorize photobak to access your photos. Subsequent runs use the previously-stored credentials, so you won't be prompted again. However, you must continue to make your API key available in environment variables.

To specify more accounts, just rinse and repeat:

```bash
$ photobak -googlephotos you@yours.com -googlephotos them@theirs.com
```

Photobak stores all content in a repository. The default repository is "./photos_backup", relative to the current working directory. You can change this with the `-repo` flag: `-repo ~/backups`. Inside the repository, a `.db` file is created. This is Photobak's index. Don't delete it. Don't change or move the files in the repository, or Photobak will probably try to re-download them next time because of integrity checks. It keeps an accounting of all files in the repository.

A photo or video may appear in more than one album. This is fine, but Photobak will not store more than one copy of a photo or video. Instead, it will write the path to where the file can be found out to a file in the album called "others.txt". You can follow those paths to find the rest of the photos for an album.

After a full backup has completed, future backups will be much quicker. Because of this, you can run Photobak as often as you like (I usually do once per day, see below for running on a schedule).

By default, photobak only stores what it needs to do its archiving functions. You can tell it to store everything the cloud service returns with the `-everything` flag, but be aware it will increase the size of the index. For Google Photos, this extra information is things like links to thumbnails of various sizes, whether comments are enabled, license details, etc. You do not need to use this flag to store photo captions, names, or GPS coordinates from EXIF, because Photobak extracts and saves those regardless (they are considered valuable metadata).

Repositories are portable. You can move them around, back them up, etc, so long as you do not disturb the structure or contents within a repository.

Photobak never mutates remote storage. It is read-only on the cloud service.

## Additive vs. Destructive

By default, Photobak runs backup operations: it only adds to the local index. Photobak will not delete photos or albums once they have been downloaded.

However, you can use the `-prune` flag to delete items locally that no longer appear in your cloud service. With this flag, Photobak will perform a regular backup operation, then delete local items that have disappeared remotely. This way, you can keep disk space under control.

The `-prune` command is destructive, so make sure you trust that the API is healthy before you run it (or have a backup of your backup). I usually don't run `-prune` as often as I do regular backups.

## Run on a Schedule

Photobak can run indefinitely and perform its backup operations on a regular schedule with the `-every` option: `-every 1d`. This will run the command every 24 hours. Valid units are `m`, `h`, `d` for minute, hour, and day, respectively. You should run this in the background since it will block forever.

You could also use cron, just don't use the `-every` option with a cron command. If a backup is still running when the next cron executes, the second cron command will fail since the database is locked.

## Logging and Error Handling

By default, logs are written to standard error (stderr). You can specify a file (or stdout) with the `-log` flag: `-log photobak.log`. Log files are rolled when they get large, and old log files will be deleted after 90 days. A maximum of 10 log files will be kept.

Only errors are logged. An error is defined to be a failed operation that could result in lost data should the backup be needed while in the error state.

An error will not terminate more than its scope. For example, a network error downloading a file will not terminate the whole program; it will go on to try the next file. A problem with credentials, however, will prevent all future operations with the cloud service, so the program will terminate.

Because Photobak's operations are idempotent, you should be able to just run the command again (after assessing the error) to retry.

Only one Photobak instance may work on a repository at a time. If multiple invocations of photobak attempt to open the database at the same time, any other the first will get a timeout error.

## Running Headless

Photobak must be authorized to access your accounts before it can be of any use, and obtaining authorization for services that use OAuth require opening a browser tab for the user to grant access. This does not work so well headless.

You can run your photobak command with the `-authonly` flag, and it will obtain any needed credentials for all configured accounts and store them in the database. You can then copy the database to your remote machine and use its folder as the repository; the credentials in the repo's database that you already obtained will be used.

## Caveats

This program is designed to work with various cloud providers in a generic way, and each one has little things to be aware of.

### Google Photos

- There is no Google Photos API; it uses a zombied version of the [Picasa Web Albums API](https://developers.google.com/picasa-web/docs/2.0/developers_guide_protocol) which is somewhat crippled. It still works for now, and one advantage is that you don't have to mirror your Google Photos in Google Drive for this program to work.

- Some users [have reported](https://code.google.com/p/gdata-issues/issues/detail?id=7004) that a [maximum of ~10,000 photos can be downloaded](https://github.com/camlistore/camlistore/issues/874) per album. It is still unclear why this is; even Google employees are hitting this. Google Photos puts all your "instant upload" (auto backup) photos into a single album called "Auto Backup". So if you take most of your photos on your phone and they get uploaded to Google Photos, you may hit this limit and there is no way to get photos older than the most recent 10k unless you put them into albums you create. This issue becomes irrelevant as you run backups regularly, assuming later you don't go way back and add really old photos to your cloud service that you don't already have locally.

- Media may be available in several formats and sizes for a single item. Photobak will try to get the largest .mp4 video file, if available. If not, it will get the largest video even if it is a .flv file. If there is no video available, it tries the highest-resolution _anything_ it can find.

- Filenames for albums and photos are sanitized to remove special characters that sometimes appear but may not play nicely with the file system. For example, "5:5.jpg" becomes "55.jpg".


## Motivation

I have an Android phone, and I love using Google Photos. It's amazing: free, unlimited photo storage that is automatically indexed and organized and searchable. When I take a picture on my phone, it goes straight to the cloud, and then my phone frees up space. This is all automatic, and it's great.

The problem is, what if I lose access to my Google account? I have no local copy of my memories. They skipped my computer and went straight to the cloud. This program is designed for users who are too busy to manually download all their photos on a regular basis but still want a local copy of them, just in case.

