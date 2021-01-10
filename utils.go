package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
)

// copyFile is safety wrapper around copyFileData.
// If the src file doesn't exist, return success.
// If src and dst files exist, and are the same, then return success.
// Else, copy the file contents from src to dst.
// Note that, it cannot copy non-regular files (e.g., directories, symlinks, devices, etc.)
func copyFile(src, dst string) (err error) {
	// Stat source file
	srcFileInfo, err := os.Stat(src)
	if err != nil {
		// If the file doesn't exist, return success.
		if os.IsNotExist(err) {
			return nil
		}
		return
	}

	// Check if the source file is regular
	if !srcFileInfo.Mode().IsRegular() {
		return fmt.Errorf("Cannot copy! Source is a non-regular file %s (%q)", srcFileInfo.Name(), srcFileInfo.Mode().String())
	}

	// Stat destination file
	destFileInfo, err := os.Stat(dst)
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
	} else {
		// Check if the destination file is regular
		if !(destFileInfo.Mode().IsRegular()) {
			return fmt.Errorf("Cannot copy! Destination is a non-regular file %s (%q)", destFileInfo.Name(), destFileInfo.Mode().String())
		}
		if os.SameFile(srcFileInfo, destFileInfo) {
			return
		}
	}

	// Copy the source file to the destination
	err = copyFileData(src, dst)
	if err != nil {
		return fmt.Errorf("Error copying! %v", err)
	}

	return
}

// copyFileData copies the contents of the source file to the destination file.
// The file will be created if it doesn't exist. Replace file contents on destination if the file already exists.
func copyFileData(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("Cannot open source file %s: %v", src, err)
	}
	defer func() {
		cerr := in.Close()
		if err == nil {
			err = cerr
		}
	}()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("Cannot create destination file %s: %v", dst, err)
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()

	// Copy the file from in(src) to out(dst)
	_, err = io.Copy(out, in)
	if err != nil {
		return fmt.Errorf("Cannot copy file: %v", err)
	}

	// Flush to storage
	err = out.Sync()
	if err != nil {
		return fmt.Errorf("Cannot flush data to disk: %v", err)
	}

	return
}

// fileToLines opens a file and returns all the lines in the file
func fileToLines(filePath string) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return linesFromReader(f)
}

// linesFromReader returns all the lines from open file reader
func linesFromReader(r io.Reader) ([]string, error) {
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}
