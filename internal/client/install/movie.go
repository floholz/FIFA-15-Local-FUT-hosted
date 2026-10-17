package install

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// The boot intro is an EA movie: an MVhd header (vp60, u16 width, u16 height,
// u32 frame count, ...), then MV0K/MV0F video chunks interleaved with the
// SCHl/SCDl audio chunks, each chunk "tag + u32 little-endian size". The game
// waits for the movie to finish, so a missing file hangs it on the legal screen;
// a movie cut down to a few frames ends right away instead.

// ShortMovieFrames is how many video frames a shortened intro keeps (0.1 s).
const ShortMovieFrames = 3

// MovieFrames returns the frame count from an EA movie's MVhd header.
func MovieFrames(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var hdr [20]byte
	if _, err := f.Read(hdr[:]); err != nil {
		return 0, err
	}
	if string(hdr[:4]) != "MVhd" || string(hdr[8:12]) != "vp60" {
		return 0, errors.New("not an EA VP6 movie")
	}
	return int(binary.LittleEndian.Uint32(hdr[16:20])), nil
}

// ShortenMovie keeps the header, the audio header and the first n video frames
// (with the audio chunks between them) and patches the frame count.
func ShortenMovie(data []byte, n int) ([]byte, error) {
	var out []byte
	frames := 0
	for off := 0; off+8 <= len(data); {
		tag := string(data[off : off+4])
		size := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		if size < 8 || off+size > len(data) {
			return nil, fmt.Errorf("bad chunk %q at %d", tag, off)
		}
		chunk := data[off : off+size]
		off += size
		switch tag {
		case "MVhd":
			if size < 20 || string(chunk[8:12]) != "vp60" {
				return nil, errors.New("not an EA VP6 movie")
			}
			hdr := append([]byte{}, chunk...)
			binary.LittleEndian.PutUint32(hdr[16:20], uint32(n))
			out = append(out, hdr...)
		case "MV0K", "MV0F":
			if frames == n {
				return out, nil
			}
			frames++
			out = append(out, chunk...)
		default:
			out = append(out, chunk...)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("empty movie")
	}
	return out, nil
}

func (m *Manifest) movieNeedsShortening(gameDir, rel string) bool {
	n, err := MovieFrames(filepath.Join(gameDir, filepath.FromSlash(rel)))
	return err == nil && n > ShortMovieFrames
}

// shortenMovies backs up each intro movie once, then replaces it with a short cut.
func (m *Manifest) shortenMovies(gameDir, backup string, progress func(string)) error {
	for _, rel := range m.ShortenMovies {
		if !m.movieNeedsShortening(gameDir, rel) {
			continue // absent, not a movie we know, or already short
		}
		path := filepath.Join(gameDir, filepath.FromSlash(rel))
		bak := filepath.Join(backup, filepath.FromSlash(rel))
		if _, err := os.Stat(bak); err != nil {
			progress("Backing up " + rel)
			if err := copyFile(path, bak); err != nil {
				return fmt.Errorf("backing up %s: %w", rel, err)
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		short, err := ShortenMovie(data, ShortMovieFrames)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		progress("Skipping the intro: " + rel)
		tmp := path + ".ouft-part"
		if err := os.WriteFile(tmp, short, 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	return nil
}
