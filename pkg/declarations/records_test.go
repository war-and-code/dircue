package declarations

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestProjectRecordsPreserveEligibilityWithoutChangingReport(t *testing.T) {
	var ordinary []byte
	for _, enabled := range []bool{false, true} {
		c := New("directory", "", 0)
		if enabled {
			c.EnableProjectRecords()
		}
		if c.ProjectRecords() != nil {
			t.Fatal("records available before Finish")
		}
		c.Add("good/pyproject.toml", candidateFor("good/pyproject.toml", "[project]\nname = 'good'\nrequires-python = '>=3.11'\n"))
		c.Add("bad/pyproject.toml", candidateFor("bad/pyproject.toml", "[project\n"))
		r, err := c.Finish(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if !enabled {
			ordinary = data
			c.EnableProjectRecords()
			if c.ProjectRecords() != nil {
				t.Fatal("late opt-in resurrected discarded parser state")
			}
			continue
		}
		if string(data) != string(ordinary) {
			t.Fatal("eligibility tracking changed public declaration report")
		}
		records := c.ProjectRecords()
		if len(records) != 2 || records[0].Project.ID != "bad/pyproject.toml" || records[0].Parsed || !records[1].Parsed || !records[1].Complete {
			t.Fatalf("wrong eligibility: %+v", records)
		}
		records[1].Project.Requirements[0].Value = "mutated"
		again := c.ProjectRecords()
		if again[1].Project.Requirements[0].Value == "mutated" {
			t.Fatal("caller mutated retained records")
		}
		after, _ := json.Marshal(r)
		if string(after) != string(data) {
			t.Fatal("caller mutated public report through records")
		}
	}
}

func TestProjectRecordsDoNotRestoreOmittedInput(t *testing.T) {
	c := New("directory", "", 4)
	c.EnableProjectRecords()
	c.Add("pyproject.toml", candidateFor("pyproject.toml", "[project]\nname='large'\n"))
	r, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "partial" || len(c.ProjectRecords()) != 0 {
		t.Fatal("omitted input became eligible")
	}
}

func TestProjectRecordsIncompleteDocument(t *testing.T) {
	c := New("directory", "", 0)
	c.EnableProjectRecords()
	data := "[project]\nname = 'many'\ndependencies = [" + strings.Repeat("'dep',", MaxObservationsPerManifest+10) + "]\n"
	c.Add("pyproject.toml", candidateFor("pyproject.toml", data))
	if _, err := c.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	records := c.ProjectRecords()
	if len(records) != 1 || records[0].Complete {
		t.Fatalf("missing per-document incompleteness: %+v", records)
	}
}

func TestProjectRecordsPreserveEmptyMavenAggregatorMetadataWithoutChangingReport(t *testing.T) {
	const source = "<project><modelVersion>4.0.0</modelVersion><modules/></project>"
	var ordinary []byte
	for _, enabled := range []bool{false, true} {
		c := New("directory", "", 0)
		if enabled {
			c.EnableProjectRecords()
		}
		c.Add("pom.xml", candidateFor("pom.xml", source))
		r, err := c.Finish(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if !enabled {
			ordinary = data
			continue
		}
		if string(data) != string(ordinary) {
			t.Fatal("assessment-only Maven metadata changed public declaration JSON")
		}
		records := c.ProjectRecords()
		if len(records) != 1 || !records[0].WorkspaceDeclared {
			t.Fatalf("empty Maven group metadata missing: %+v", records)
		}
	}
}

func TestProjectRecordsUnavailableAfterFailedFinish(t *testing.T) {
	c := New("directory", "", 0)
	c.EnableProjectRecords()
	c.Add("pyproject.toml", candidateFor("pyproject.toml", "[project]\nname='x'\n"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Finish(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if c.ProjectRecords() != nil {
		t.Fatal("failed collection exposed eligible records")
	}
}
