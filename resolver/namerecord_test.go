package resolver

import "testing"

func TestNameRecordStorage(t *testing.T) {
	t.Parallel()

	testDomain := "Mk35mMqOWEHXSMk11MYcbjLOjTE8PQvDiAVUxf4BvwtgR.example.com."
	testQuestion := "A"

	testNameRecord := &NameRecord{
		Domain:   testDomain,
		Question: testQuestion,
		Resolver: &ResolverInfo{
			Type: "dns",
		},
	}

	err := testNameRecord.Save()
	if err != nil {
		t.Fatal(err)
	}

	r, err := GetNameRecord(testDomain, testQuestion)
	if err != nil {
		t.Fatal(err)
	}

	if r.Domain != testDomain || r.Question != testQuestion {
		t.Fatal("mismatch")
	}
}
