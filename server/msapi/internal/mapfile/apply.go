package mapfile

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const MapfileName = "services.map"

// tempPattern names temporary mapfiles; MapServer only loads paths ending
// in .map, and the leading dot keeps them apart from services.map.
const tempPattern = ".services-*.tmp.map"

func MapfilePath(mapfileDir string) string { return filepath.Join(mapfileDir, MapfileName) }

// Validator checks a mapfile on disk; nil error means MapServer can load it.
type Validator func(ctx context.Context, path string) error

// RejectedError means MapServer refused the generated mapfile, i.e. the
// change that produced it is invalid (as opposed to an I/O failure).
type RejectedError struct{ Msg string }

func (e *RejectedError) Error() string { return "mapfile rejected by MapServer: " + e.Msg }

// MapservValidator asks MapServer itself to load the mapfile and answer a
// WMS GetCapabilities (parses everything, reads no data). It runs the
// mapserv binary from the MapServer base image in CLI mode. configFile is a
// MapServer config that allows map= paths under the mapfile dir
// (see msapi/Dockerfile) [verify mapserv CLI behaviour in 8.6].
func MapservValidator(configFile string) Validator {
	return func(ctx context.Context, path string) error {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		qs := "QUERY_STRING=map=" + path + "&SERVICE=WMS&VERSION=1.3.0&REQUEST=GetCapabilities"
		cmd := exec.CommandContext(ctx, "mapserv", "-nh", qs)
		cmd.Env = append(os.Environ(), "MAPSERVER_CONFIG_FILE="+configFile)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		runErr := cmd.Run()
		text := out.String()
		if strings.Contains(text, "<WMS_Capabilities") && !strings.Contains(text, "ServiceException") {
			return nil
		}
		if errors.Is(runErr, exec.ErrNotFound) {
			return errors.New("mapserv not found; run msapi in its Docker image or set MSAPI_VALIDATE=off")
		}
		return &RejectedError{Msg: mapserverMessage(text, runErr)}
	}
}

var msgRe = regexp.MustCompile(`(?s)(?:<ServiceException[^>]*>|MapServer Message:)\s*(.*?)\s*(?:</ServiceException>|$)`)

func mapserverMessage(text string, runErr error) string {
	if m := msgRe.FindStringSubmatch(text); m != nil {
		return firstLines(m[1], 5)
	}
	if strings.TrimSpace(text) != "" {
		return firstLines(text, 5)
	}
	if runErr != nil {
		return runErr.Error()
	}
	return "no capabilities document returned"
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " | ")
}

// Apply writes content as services.map: temp file in the same directory,
// validate, then rename. On any error the live mapfile is untouched
// (PLAN.md §3 rule 2).
func Apply(ctx context.Context, mapfileDir, content string, validate Validator) error {
	if err := os.MkdirAll(mapfileDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(mapfileDir, tempPattern)
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	if validate != nil {
		if err := validate(ctx, tmp.Name()); err != nil {
			return err
		}
	}
	return os.Rename(tmp.Name(), MapfilePath(mapfileDir))
}

// WorkspacePrefix names per-workspace mapfiles: ws_<name>.map, next to
// services.map so relative SYMBOLSET / FONTSET / icon paths keep working.
const WorkspacePrefix = "ws_"

func WorkspaceFile(name string) string { return WorkspacePrefix + name + ".map" }

// File is one mapfile to apply: Name is relative to the mapfile dir.
type File struct{ Name, Content string }

// ApplySet writes several mapfiles at once: every file is written to a temp
// file and validated first, and only if all pass are they renamed into
// place. Afterwards ws_*.map files not in the set (deleted workspaces) are
// removed. On error no live mapfile changes.
func ApplySet(ctx context.Context, mapfileDir string, files []File, validate Validator) error {
	if err := os.MkdirAll(mapfileDir, 0o755); err != nil {
		return err
	}
	temps := make([]string, len(files))
	defer func() {
		for _, t := range temps {
			if t != "" {
				os.Remove(t) // no-op after a successful rename
			}
		}
	}()
	for i, f := range files {
		tmp, err := os.CreateTemp(mapfileDir, tempPattern)
		if err != nil {
			return err
		}
		temps[i] = tmp.Name()
		_, werr := tmp.WriteString(f.Content)
		if werr == nil {
			werr = tmp.Sync()
		}
		if cerr := tmp.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return werr
		}
		if err := os.Chmod(tmp.Name(), 0o644); err != nil {
			return err
		}
		if validate != nil {
			if err := validate(ctx, tmp.Name()); err != nil {
				if r, ok := err.(*RejectedError); ok && f.Name != MapfileName {
					return &RejectedError{Msg: f.Name + ": " + r.Msg}
				}
				return err
			}
		}
	}
	keep := map[string]bool{}
	for i, f := range files {
		if err := os.Rename(temps[i], filepath.Join(mapfileDir, f.Name)); err != nil {
			return err
		}
		keep[f.Name] = true
	}
	old, _ := filepath.Glob(filepath.Join(mapfileDir, WorkspacePrefix+"*.map"))
	for _, p := range old {
		if !keep[filepath.Base(p)] {
			os.Remove(p)
		}
	}
	return nil
}

// RenderMapserv renders a WMS request against a temporary mapfile with the
// mapserv CLI (used for previews of unsaved styles, which the public
// MapServer cannot load because of MS_MAP_NO_PATH). Returns PNG bytes.
func RenderMapserv(ctx context.Context, configFile, mapfileDir, content string, params url.Values) ([]byte, error) {
	tmp, err := os.CreateTemp(mapfileDir, tempPattern)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 180*time.Second) // like MapServer's IO_TIMEOUT
	defer cancel()
	cmd := exec.CommandContext(ctx, "mapserv", "-nh", "QUERY_STRING=map="+tmp.Name()+"&"+params.Encode())
	cmd.Env = append(os.Environ(), "MAPSERVER_CONFIG_FILE="+configFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	out := stdout.Bytes()
	if bytes.HasPrefix(out, []byte("\x89PNG")) {
		return out, nil
	}
	if errors.Is(runErr, exec.ErrNotFound) {
		return nil, errors.New("mapserv not found; previews need msapi's Docker image")
	}
	return nil, errors.New(mapserverMessage(string(out)+stderr.String(), runErr))
}
