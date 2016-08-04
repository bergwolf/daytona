package main

import (
	"archive/tar"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/drone/routes"
	xattr "github.com/ivaxer/go-xattr"
)

var volPath, volFilename, cookie string

func main() {
	var (
		ok  bool
		err error
	)
	if volPath, ok = os.LookupEnv("INIT_VOLUME_PATH"); !ok {
		fmt.Fprintf(os.Stderr, "cannot find INIT_VOLUME_PATH environment variable\n")
		return
	}
	if volFilename, ok = os.LookupEnv("INIT_VOLUME_FILENAME"); !ok {
		fmt.Fprintf(os.Stderr, "cannot find INIT_VOLUME_FILENAME environment variable\n")
		return
	}
	if cookie, ok = os.LookupEnv("INIT_VOLUME_COOKIE"); !ok {
		fmt.Fprintf(os.Stderr, "cannot find INIT_VOLUME_COOKIE environment variable\n")
		return
	}
	fmt.Fprintf(os.Stdout, "environment: %s %s %s\n", volPath, volFilename, cookie)

	mux := routes.New()
	mux.Post("/:vol", tarUploader)
	http.Handle("/", mux)
	err = http.ListenAndServe(":1985", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "exited: %s\n", err.Error())
	}
}

func tarUploader(w http.ResponseWriter, r *http.Request) {
	var (
		userCookie, vol string
		err             error
	)

	defer func() {
		if err != nil {
			w.WriteHeader(http.StatusNotAcceptable)
			w.Write([]byte(err.Error()))
		}
	}()

	params := r.URL.Query()
	vol = params.Get(":vol")
	userCookie = params.Get("cookie")
	fmt.Println("vol", vol, "cookie", userCookie)
	if userCookie != cookie {
		err = fmt.Errorf("bad cookie")
		return
	}

	if r.Header.Get("Content-Type") != "application/x-tar" {
		err = fmt.Errorf("Bad request content type")
		return
	}

	destDir := "/" + volPath + "/" + vol
	if info, err := os.Stat(destDir); err != nil {
		fmt.Printf("stat failed %s\n", err.Error())
		return
	} else if !info.IsDir() {
		fmt.Printf("%s is not dir\n", destDir)
		err = fmt.Errorf("404 page not found")
		return
	}

	if r.Body == nil {
		err = fmt.Errorf("No data sent")
		return
	}

	tarReader := tar.NewReader(r.Body)
	for {
		h, err := tarReader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return
		}
		if err = saveTarFile(destDir, h, tarReader); err != nil {
			return
		}
	}

	w.Write([]byte("success"))
}

func saveTarFile(dir string, h *tar.Header, r io.Reader) (err error) {
	filePath := dir + "/" + h.Name
	info := h.FileInfo()
	fmt.Printf("saving new file %s\n", filePath)
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		if err = os.Symlink(h.Linkname, filePath); err != nil {
			return err
		}
	case info.Mode().IsDir():
		if err = os.Mkdir(filePath, info.Mode().Perm()); err != nil {
			return err
		}
	case info.Mode().IsRegular():
		fallthrough
	default: // Treat special files as normal files
		fw, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err = io.Copy(fw, r); err != nil {
			return err
		}
	}

	// ownership and xattrs
	if err = os.Lchown(filePath, h.Uid, h.Gid); err != nil {
		return err
	}
	for key, val := range h.Xattrs {
		if err = xattr.Set(filePath, key, []byte(val)); err != nil {
			return err
		}
	}

	return nil
}