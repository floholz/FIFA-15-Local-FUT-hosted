package install

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func chunk(tag string, body []byte) []byte {
	c := make([]byte, 8, 8+len(body))
	copy(c, tag)
	binary.LittleEndian.PutUint32(c[4:], uint32(8+len(body)))
	return append(c, body...)
}

// fakeMovie builds MVhd + SCHl + frames x (video + SCDl audio).
func fakeMovie(frames int) []byte {
	hdr := make([]byte, 24)
	copy(hdr, "vp60")
	binary.LittleEndian.PutUint16(hdr[4:], 1920)
	binary.LittleEndian.PutUint16(hdr[6:], 1072)
	binary.LittleEndian.PutUint32(hdr[8:], uint32(frames))
	m := append(chunk("MVhd", hdr), chunk("SCHl", []byte("GSTR-audio-header"))...)
	for i := 0; i < frames; i++ {
		tag := "MV0F"
		if i%10 == 0 {
			tag = "MV0K"
		}
		m = append(m, chunk(tag, []byte{byte(i), 1, 2, 3})...)
		m = append(m, chunk("SCDl", []byte{byte(i), 9})...)
	}
	return m
}

func TestShortenMovie(t *testing.T) {
	short, err := ShortenMovie(fakeMovie(361), 3)
	if err != nil {
		t.Fatal(err)
	}
	// header + audio header + 3 x (frame + audio)
	want := len(chunk("MVhd", make([]byte, 24))) + len(chunk("SCHl", []byte("GSTR-audio-header"))) + 3*(12+10)
	if len(short) != want {
		t.Fatalf("len = %d, want %d", len(short), want)
	}
	path := filepath.Join(t.TempDir(), "intro.vp6")
	os.WriteFile(path, short, 0o644)
	if n, err := MovieFrames(path); err != nil || n != 3 {
		t.Fatalf("frames = %d, %v", n, err)
	}
	if _, err := ShortenMovie([]byte("not a movie at all"), 3); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestInstallShortensIntroAndRestoreBringsItBack(t *testing.T) {
	payload, game, rt := t.TempDir(), t.TempDir(), t.TempDir()
	orig := fakeMovie(361)
	write(t, filepath.Join(game, "data/movies/intro.vp6"), string(orig))
	m := &Manifest{ShortenMovies: []string{"data/movies/intro.vp6", "data/movies/missing.vp6"}}
	if got := m.Outdated(game); len(got) != 1 {
		t.Fatalf("outdated = %v", got)
	}
	if err := m.Install(payload, game, rt, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if n, _ := MovieFrames(filepath.Join(game, "data/movies/intro.vp6")); n != 3 {
		t.Fatalf("intro not shortened: %d frames", n)
	}
	if got := m.Outdated(game); len(got) != 0 {
		t.Fatalf("still outdated: %v", got)
	}
	if err := m.Restore(game, rt, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(game, "data/movies/intro.vp6")); got != string(orig) {
		t.Fatal("restore did not bring the original intro back")
	}
	if _, err := os.Stat(filepath.Join(game, "data/movies/missing.vp6")); err == nil {
		t.Fatal("restore created a movie the game never had")
	}
}
