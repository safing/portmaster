package binmeta

import (
	"context"
	"os"
	"testing"
)

func TestFindIcon(t *testing.T) {
	if testing.Short() {
		t.Skip("test meant for compiling and running on desktop")
	}
	t.Parallel()

	binName := os.Args[len(os.Args)-1]
	t.Logf("getting name and icon for %s", binName)
	png, name, err := getIconAndNamefromRSS(context.Background(), binName)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("name: %s", name)
	err = os.WriteFile("icon.png", png, 0o0600)
	if err != nil {
		t.Fatal(err)
	}
}
