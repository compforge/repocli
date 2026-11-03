package history

import (
	"errors"
	"os"
	"path/filepath"
)

func atomicFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func (h *Store) SaveInputs(id string, data []byte) error {
	return atomicFile(filepath.Join(h.dir, "inputs", id+".json"), data)
}

func (h *Store) LoadInputs(id string) ([]byte, error) {
	if !validID(id) {
		return nil, errors.New("invalid input record ID")
	}
	return os.ReadFile(filepath.Join(h.dir, "inputs", id+".json"))
}

func validID(id string) bool {
	if id == "" {
		return false
	}
	for _, c := range id {
		if !(c >= 'A' && c <= 'Z' || c >= '2' && c <= '7') {
			return false
		}
	}
	return true
}

func (h *Store) SaveOutput(id, stdout, stderr string) error {
	if err := atomicFile(filepath.Join(h.dir, "logs", id, "stdout"), []byte(stdout)); err != nil {
		return err
	}
	return atomicFile(filepath.Join(h.dir, "logs", id, "stderr"), []byte(stderr))
}

func (h *Store) LoadOutput(id string) (string, string, error) {
	if !validID(id) {
		return "", "", errors.New("invalid run ID")
	}
	out, err := os.ReadFile(filepath.Join(h.dir, "logs", id, "stdout"))
	if err != nil {
		return "", "", err
	}
	stderr, err := os.ReadFile(filepath.Join(h.dir, "logs", id, "stderr"))
	if err != nil {
		return "", "", err
	}
	return string(out), string(stderr), nil
}
