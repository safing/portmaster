package renameio_test

import (
	"fmt"
	"log"

	"github.com/safing/portmaster/base/utils/renameio"
)

func ExampleTempFile_justone() { //nolint:testableexamples
	persist := func(temperature float64) error {
		t, err := renameio.TempFile("", "/srv/www/metrics.txt")
		if err != nil {
			return err
		}
		defer func() {
			_ = t.Cleanup()
		}()
		if _, err := fmt.Fprintf(t, "temperature_degc %f\n", temperature); err != nil {
			return err
		}
		return t.CloseAtomicallyReplace()
	}
	// Thanks to the write package, a webserver exposing /srv/www never
	// serves an incomplete or missing file.
	if err := persist(31.2); err != nil {
		log.Fatal(err)
	}
}

func ExampleTempFile_many() { //nolint:testableexamples
	// Prepare for writing files to /srv/www, effectively caching calls to
	// TempDir which TempFile would otherwise need to make.
	dir := renameio.TempDir("/srv/www")
	persist := func(temperature float64) error {
		t, err := renameio.TempFile(dir, "/srv/www/metrics.txt")
		if err != nil {
			return err
		}
		defer func() {
			_ = t.Cleanup()
		}()
		if _, err := fmt.Fprintf(t, "temperature_degc %f\n", temperature); err != nil {
			return err
		}
		return t.CloseAtomicallyReplace()
	}

	// Imagine this was an endless loop, reading temperature sensor values.
	// Thanks to the write package, a webserver exposing /srv/www never
	// serves an incomplete or missing file.
	for {
		if err := persist(31.2); err != nil {
			log.Fatal(err)
		}
	}
}
