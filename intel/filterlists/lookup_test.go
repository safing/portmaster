package filterlists

/*

func TestLookupASN(t *testing.T) {
	lists, err := LookupASNString("123")
	assert.NoError(t, err)
	assert.Equal(t, []string{"TEST"}, lists)

	lists, err = LookupASNString("does-not-exist")
	assert.NoError(t, err)
	assert.Empty(t, lists)

	defer testMarkNotLoaded()()
	lists, err = LookupASNString("123")
	assert.NoError(t, err)
	assert.Empty(t, lists)
}

func TestLookupCountry(t *testing.T) {
	lists, err := LookupCountry("AT")
	assert.NoError(t, err)
	assert.Equal(t, []string{"TEST"}, lists)

	lists, err = LookupCountry("does-not-exist")
	assert.NoError(t, err)
	assert.Empty(t, lists)

	defer testMarkNotLoaded()()
	lists, err = LookupCountry("AT")
	assert.NoError(t, err)
	assert.Empty(t, lists)
}

func TestLookupIP(t *testing.T) {
	lists, err := LookupIP(net.IP{1, 1, 1, 1})
	assert.NoError(t, err)
	assert.Equal(t, []string{"TEST"}, lists)

	lists, err = LookupIP(net.IP{127, 0, 0, 1})
	assert.NoError(t, err)
	assert.Empty(t, lists)

	defer testMarkNotLoaded()()
	lists, err = LookupIP(net.IP{1, 1, 1, 1})
	assert.NoError(t, err)
	assert.Empty(t, lists)
}

func TestLookupDomain(t *testing.T) {
	lists, err := LookupDomain("example.com")
	assert.NoError(t, err)
	assert.Equal(t, []string{"TEST"}, lists)

	lists, err = LookupDomain("does-not-exist")
	assert.NoError(t, err)
	assert.Empty(t, lists)

	defer testMarkNotLoaded()()
	lists, err = LookupDomain("example.com")
	assert.NoError(t, err)
	assert.Empty(t, lists)
}

// testMarkNotLoaded ensures that functions believe
// filterlists are not yet loaded. It returns a
// func that restores the previous state.
func testMarkNotLoaded() func() {
	if isLoaded() {
		filterListsLoaded = make(chan struct{})
		return func() {
			close(filterListsLoaded)
		}
	}

	return func() {}
}

// testMarkLoaded is like testMarkNotLoaded but ensures
// isLoaded() return true. It returns a function to restore
// the previous state.
func testMarkLoaded() func() {
	if !isLoaded() {
		close(filterListsLoaded)
		return func() {
			filterListsLoaded = make(chan struct{})
		}
	}

	return func() {}
}
*/
