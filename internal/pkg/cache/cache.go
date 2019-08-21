package cache

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/pkg/errors"
)

type Elements map[int]bool

type Cache struct {
	FilePath  string
	Processed Elements
}

func NewCache(cachePath string) (Cache, error) {
	elements, err := ReadCache(cachePath)
	if err != nil {
		return Cache{}, errors.Wrap(err, "error reading cache")
	}

	return Cache{
		FilePath:  cachePath,
		Processed: elements,
	}, nil
}

func ReadCache(cachePath string) (Elements, error) {
	raw, err := ioutil.ReadFile(cachePath)
	if os.IsNotExist(err) {
		return Elements{}, nil
	}

	if err != nil {
		return Elements{}, errors.Wrap(err, "failed-to-read-cache-file")
	}

	var elements Elements
	err = json.Unmarshal(raw, &elements)
	if err != nil {
		return Elements{}, errors.Wrap(err, "failed-to-unmarshal-cache")
	}

	return elements, nil
}

func (c *Cache) Flush() {
	raw, err := json.Marshal(c.Processed)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	err = ioutil.WriteFile(c.FilePath, raw, 0644)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func (c *Cache) IsProcessed(buildID int) bool {
	wasProcessed, ok := c.Processed[buildID]
	return ok && wasProcessed
}

func (c *Cache) MarkProcessed(buildID int) {
	c.Processed[buildID] = true

	c.Flush()
}
