package resolver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckResolverSearchScope(t *testing.T) {
	t.Parallel()

	// should fail (invalid)
	assert.Error(t, checkSearchScope("."))
	assert.Error(t, checkSearchScope(".com."))
	assert.Error(t, checkSearchScope("com."))
	assert.Error(t, checkSearchScope(".com"))

	// should fail (too high scope)
	assert.Error(t, checkSearchScope("com"))
	assert.Error(t, checkSearchScope("net"))
	assert.Error(t, checkSearchScope("org"))
	assert.Error(t, checkSearchScope("pvt.k12.ma.us"))

	// should succeed
	assert.NoError(t, checkSearchScope("a.com"))
	assert.NoError(t, checkSearchScope("b.a.com"))
	assert.NoError(t, checkSearchScope("c.b.a.com"))
	assert.NoError(t, checkSearchScope("test.pvt.k12.ma.us"))

	assert.NoError(t, checkSearchScope("onion"))
	assert.NoError(t, checkSearchScope("a.onion"))
	assert.NoError(t, checkSearchScope("b.a.onion"))
	assert.NoError(t, checkSearchScope("c.b.a.onion"))

	assert.NoError(t, checkSearchScope("bit"))
	assert.NoError(t, checkSearchScope("a.bit"))
	assert.NoError(t, checkSearchScope("b.a.bit"))
	assert.NoError(t, checkSearchScope("c.b.a.bit"))

	assert.NoError(t, checkSearchScope("doesnotexist"))
	assert.NoError(t, checkSearchScope("a.doesnotexist"))
	assert.NoError(t, checkSearchScope("b.a.doesnotexist"))
	assert.NoError(t, checkSearchScope("c.b.a.doesnotexist"))
}
