package main

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/pkg/errors"
)

type ProductId string

var productsMap map[ProductId]struct{ GCal string }

func loadProducts(prodFile string) error {
	f, err := os.Open(prodFile)
	if err != nil {
		return errors.Wrap(err, "opening products file")
	}
	defer f.Close()

	prodsJson, err := ioutil.ReadAll(f)
	if err != nil {
		return errors.Wrap(err, "reading products file")
	}

	if err := json.Unmarshal(prodsJson, &productsMap); err != nil {
		return errors.Wrap(err, "parsing products file")
	}

	return nil
}

func products() (keys []ProductId) {
	for k := range productsMap {
		keys = append(keys, k)
	}
	return keys
}
