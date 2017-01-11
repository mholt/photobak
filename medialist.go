package photobak

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// writeToMediaListFile adds dlPath to the media list file
// in the given collection. The collection must have its
// proper repo-relative path set.
func (r *Repository) writeToMediaListFile(coll collection, dlPath string) error {
	err := os.MkdirAll(r.fullPath(coll.dirPath), 0700)
	if err != nil {
		return fmt.Errorf("making folder %s: %v", coll.dirPath, err)
	}
	mediaListFile := r.fullPath(r.mediaListPath(coll.dirPath))
	of, err := os.OpenFile(mediaListFile, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("opening media list file %s: %v", mediaListFile, err)
	}
	defer of.Close()
	_, err = fmt.Fprintln(of, dlPath)
	if err != nil {
		return fmt.Errorf("appending to media list file %s: %v", mediaListFile, err)
	}
	return nil
}

// replaceInMediaListFile goes through the media list file in dirPath (repo-relative)
// and replaces any occurrence of oldPath with newPath. If newPath is empty string,
// the line will be deleted instead.
func (r *Repository) replaceInMediaListFile(dirPath, oldPath, newPath string) error {
	permFilePath := r.fullPath(r.mediaListPath(dirPath))
	tmpFilePath := r.fullPath(r.mediaListPath(dirPath) + ".tmp")

	inFile, err := os.Open(permFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no media list file, no problem, nothing to do.
		}
		return err
	}

	outFile, err := os.Create(tmpFilePath)
	if err != nil {
		inFile.Close()
		return err
	}

	var wroteAtLeastOneEntry bool
	scanner := bufio.NewScanner(inFile)
	for scanner.Scan() {
		line := scanner.Text()
		if line == oldPath {
			if newPath == "" {
				continue // skip; we are removing this line!
			}
			fmt.Fprintln(outFile, newPath)
			wroteAtLeastOneEntry = true
			continue
		}
		fmt.Fprintln(outFile, line)
		wroteAtLeastOneEntry = true
	}
	inFile.Close()
	outFile.Close()
	if err := scanner.Err(); err != nil {
		return err
	}

	// replace original file with the updated temporary one
	err = os.Rename(tmpFilePath, permFilePath)
	if err != nil {
		return fmt.Errorf("moving temporary file into place: %v", err)
	}

	if !wroteAtLeastOneEntry {
		// the file was emptied
		return os.Remove(permFilePath)
	}

	return nil
}

func (r *Repository) mediaListHasItem(collDirPath string, dbi *dbItem) (bool, error) {
	file, err := os.Open(r.fullPath(r.mediaListPath(collDirPath)))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fpath := strings.TrimSpace(scanner.Text())
		if fpath == dbi.FilePath {
			return true, nil
		}
	}
	return false, scanner.Err()
}

// mediaListPath returns the path to the media list file for
// the given collection. The returned path is repo-relative
// if the dirPath is repo-relative (which it should be!).
// The dirPath should be the repo-relative path to the collection
// directory on disk.
func (r *Repository) mediaListPath(dirPath string) string {
	return filepath.Join(dirPath, "others.txt")
}
