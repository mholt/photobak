Photobak
========

Photobak is a media archiver. It downloads your photos and videos from cloud services like Google Photos so you have a local copy of your content. Run it on a regular basis to make sure you own all your memories.


NOTICE: Photobak has been replaced by [Timeliner](https://github.com/mholt/timeliner).
===============================================

This project is no longer maintained. It's been a good and faithful tool. I have had peace of mind knowing I have a local copy of my photo library.

With the [deprecation of the Picasa Web Albums API](https://developers.google.com/picasa-web/docs/3.0/deprecation), it was time to replace Photobak with its successor, [Timeliner](https://github.com/mholt/timeliner).

Timeliner supports Google Photos and several other services. It uses the newer Google Photos API, and was mostly inspired by Photobak.

Unfortunately, Timeliner has a completely different architecture and storage mechanism, so there is no smooth transition process from Photobak to Timeliner. You will have to start over with a new archive ("timeline") using Timeliner.

Like Photobak, Timeliner organizes your photos and videos by folder. Unlike Photobak, Timeliner organizes the folders by date instead of album.

Like Photobak, Timeliner can do integrity checks and download only changed files that already exist locally. Unlike Photobak, Timeliner uses the new Google Photos API, which doesn't inform whether a photo has changed; so the only way to get the changes is to do a full `-reprocess`.

Like Photobak, Timeliner downloads photos and videos at their largest resolution/bitrate, including all available EXIF data. Unlike Photobak, Timeliner can't access location data because the Google Photos API strips it out.

Timeliner can resume interrupted downloads where it left off -- something that Photobak was not capable of. The Google Photos API is much more reliable and better documented, so Timeliner is more efficient and capable overall.



Original README content:
========================


Features:

- Integrity checks
- De-duplication
- Fast
- Organizes photos on disk by album
- Updates photos if changes are detected
- Can run on a schedule
- Concurrent downloads
- Supports multiple accounts per service
- Idempotent operations
- Separate additive and destructive commands

Supported cloud services:

- Google Photos

More providers can be easily added by [implementing some interfaces](https://github.com/mholt/photobak/wiki/Writing-a-Client-Implementation). (Please submit a pull request if you've implemented one!)

Be sure to read the caveats below for your cloud services of choice.

## Install

Since photobak is a Go program, its binaries are static and are available for every platform. Simply [download the latest](https://github.com/mholt/photobak/releases/latest) and plop it in your PATH.

Or, to install from source:

```bash
$ go get github.com/mholt/photobak/cmd/photobak
```

For help:

```plain
$ photobak -help
Usage of photobak:
  -authonly
    	Obtain authorizations only; do not perform backups
  -concurrency int
    	How many downloads to do in parallel (default 5)
  -every string
    	How often to run this command, blocking indefinitely
  -everything
    	Whether to store all metadata returned by API for each item
  -googlephotos value
    	Add a Google Photos account to the repository
  -log string
    	Write logs to a file, stdout, or stderr (default "stderr")
  -maxalbums int
    	Maximum number of albums to process (-1 for all) (default -1)
  -maxphotos int
    	Maximum number of photos per album to process (-1 for all) (default -1)
  -prune
    	Clean up removed photos and albums
  -repo string
    	The directory in which to store the downloaded media (default "./photos_backup")
  -v	Write informational log messages to stdout
```

## Usage

To store everything in a Google Photos account, get an OAuth2 Client ID from the [Google Developer Console](https://console.developers.google.com), then:

```bash
$ export GOOGLEPHOTOS_CLIENT_ID=...
$ export GOOGLEPHOTOS_CLIENT_SECRET=...
$ photobak -googlephotos you@yours.com
```

The first time using this account, you will be redirected to a web page where you'll authorize photobak to access your photos. Subsequent runs use the previously-stored credentials, so you won't be prompted again. However, you must continue to make your client ID and secret available in environment variables.

To specify more accounts, just rinse and repeat:

```bash
$ photobak -googlephotos you@yours.com -googlephotos them@theirs.com
```

Photobak stores all content in a repository. The default repository is "./photos_backup", relative to the current working directory. You can change this with the `-repo` flag: `-repo ~/backups`. Inside the repository, a `.db` file is created. This is Photobak's index. Don't delete it. Don't change or move the files in the repository, or Photobak will probably try to re-download them next time because of integrity checks. It keeps an accounting of all files in the repository.

A photo or video may appear in more than one album. This is fine, but Photobak will not store more than one copy of a photo or video. Instead, it will write the path to where the file can be found out to a file in the album called "others.txt". You can follow those paths to find the rest of the photos for an album.

After a full backup has completed, future backups will be much quicker. Because of this, you can run Photobak as often as you like (I usually do once per day, see below for running on a schedule). Remote items will be checked for changes each time you run a backup. If the service's API reports any changes to a photo from when you downloaded it, Photobak will update the item on disk.

By default, photobak only stores what it needs to do its archiving functions and a few valuable metadata fields. You can tell it to store everything the cloud service returns with the `-everything` flag, but be aware it will increase the size of the database. For Google Photos, this would be things like links to thumbnails of various sizes, whether comments are enabled, license details, etc. You do not need to use this flag to store photo captions, names, or GPS coordinates from EXIF, because Photobak extracts and saves those regardless (they are considered valuable metadata).

Repositories are portable. You can move them around, back them up, etc, so long as you do not disturb the structure or contents within a repository.

Photobak never mutates your cloud storage. It is read-only to the online service.

## Additive vs. Destructive

By default, Photobak runs backup operations: it only adds to the local index. Photobak will not delete or move photos or albums once they have been downloaded.

However, you can use the `-prune` flag to delete items locally that no longer appear in your cloud service. With this flag, Photobak will NOT perform a regular backup operation. Instead, it will query the API and delete items locally that have disappeared remotely. This way, you can keep disk space under control.

The `-prune` option is destructive, so make sure you trust that the API is healthy before you run it (or have a backup of your backup). I usually don't run `-prune` as often as I do regular backups.

## Run on a Schedule

Photobak can run indefinitely and perform its backup operations on a regular schedule with the `-every` option: `-every 1d`. This will run the command every 24 hours. Valid units are `m`, `h`, `d` for minute, hour, and day, respectively. You should run this in the background since it will block forever.

You could also use cron, but don't use the `-every` option with a cron command. If a backup is still running when the next cron executes, the second cron command will fail since the database is locked (this is normal).

To get an idea of execution time: my photo library of ~4,000 items downloaded on a fast network with `-concurrency 20` finished in a little over an hour. The final repository size was 16 GB (after de-duplication).

## Logging and Error Handling

By default, logs are written to standard error (stderr). You can specify a file (or stdout) with the `-log` flag: `-log photobak.log`. Log files are rolled when they get large, and old log files will be deleted after 90 days. A maximum of 10 log files will be kept.

Only errors are logged. An error is defined to be a failed operation that could result in lost data should the backup be needed while in the error state.

An error will not terminate more than its scope. For example, a network error downloading a file will not terminate the whole program; it will go on to try the next file. A problem with credentials, however, will prevent all future operations with the cloud service, so the program will terminate.

Because Photobak's operations are idempotent, you should be able to just run the command again (after assessing the error) to retry.

Only one Photobak instance may work on a repository at a time. If multiple invocations of photobak attempt to open the database at the same time, any other the first will get a timeout error.

You can get informational log messages with the `-v` flag. This will output a lot of information to stdout; do not use this with unsupervised executions.

## Running Headless

Photobak must be authorized to access your accounts before it can be of any use. Obtaining authorization for services that use OAuth requires opening a browser tab for the user to grant access. This does not work so well over SSH.

On your local machine, run photobak with the `-authonly` flag, and it will obtain any needed credentials for all configured accounts and store them in the database. You can then copy the database to your remote machine and use its folder as the repository; the credentials in the repo's database that you already obtained will be used.

## Caveats

This program is designed to work with various cloud providers in a generic way, and each service will have its quirks. These shouldn't be dealbreakers (otherwise I wouldn't add support for the service) but you should be aware of them.

### Google Photos

- There is no Google Photos API; it uses a zombied version of the [Picasa Web Albums API](https://developers.google.com/picasa-web/docs/2.0/developers_guide_protocol) which is somewhat crippled. It still works for now, and one advantage is that you don't have to mirror your Google Photos in Google Drive for this program to work.

- Some users [have reported](https://code.google.com/p/gdata-issues/issues/detail?id=7004) that a [maximum of ~10,000 photos can be downloaded](https://github.com/camlistore/camlistore/issues/874) per album. It is still unclear why this is; even Google employees are hitting this. Google Photos puts all your "instant upload" (auto backup) photos into a single album called "Auto Backup". So if you take most of your photos on your phone and they get uploaded to Google Photos, you may hit this limit and there is no way to get photos older than the most recent 10k unless you put them into albums you create. This issue becomes irrelevant as you run backups regularly, assuming later you don't go way back and add really old photos to your cloud service that you don't already have locally.

- Unbelievably, Google Photos does not expose unique IDs to photos in your account. It assigns IDs to unique photos _in albums_, but this is "too" unique, since the same photo may appear in multiple albums. Here, we rely on Photobak's de-duplication features. After a duplicate file is downloaded, it will be replaced with an entry in a text file that points to where it can already be found on disk. We could use another ID I found in the exif tag supplied by the API: the exif ID. This ID is more correctly unique per-photo, except sometimes it is _not unique enough_. But I only saw overlap on an edit (from an external editing app/program) of the same photo, so if one was overwritten (which it was), I still had the picture, just one variant instead of two. This actually works better as far as saving bandwidth and disk space and I was torn for days trying to decide which to use. But for now we use Google Photos' ID field.

- Sometimes, I've noticed that the same, unedited photo in my stream that is shared in different albums can not only have a different ID as mentioned above, but also a different checksum! Bizarre. Visually they looked identical, and they had the same dimensions, but when I inspected the bytes, one was a few hundred bytes shorter than the other. What's more perplexing is that both photos were exactly identical, byte-for-byte, until line 88443 of the hexdump. Then they were completely different. I've also seen sometimes that photos shared from other accounts that you add to your library can sometimes have different sizes depending on the download URL.

- Media may be available in several formats and sizes for a single item. Photobak will try to get the largest .mp4 video file, if available. If not, it will get the largest video even if it is a .flv or other type of file. If there is no video available, it tries the highest-resolution _anything_ it can find.

- Filenames for albums and photos are sanitized to remove special characters that sometimes appear but may not play nicely with the file system. For example, "5:5.jpg" becomes "55.jpg".

- Items that are shared with you but are not in your library will not be downloaded unless you click the "Add to Library" button on those items in Google Photos.


## Motivation

I have an Android phone, and I love using Google Photos. It's amazing: free, unlimited photo storage that is automatically indexed and organized and searchable. When I take a picture on my phone, it goes straight to the cloud, and then my phone frees up space. This is all automatic, and it's great.

But if I lose access to my Google account, I have no local copy of my memories. This program is designed for users who are too busy to manually download all their photos on a regular basis but still want a local copy of them, just in case.
