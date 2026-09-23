package atlas_test

import (
	"encoding/json"
	"testing"

	"dircue/internal/atlas"
)

func TestLoadReturnsValidCards(t *testing.T) {
	cards, err := atlas.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cards.SchemaVersion == "" {
		t.Error("schema_version is empty")
	}
	if cards.LabelSource == "" {
		t.Error("label_source is empty")
	}
	if cards.ScopeNote == "" {
		t.Error("scope_note is empty")
	}
}

func TestRawJSONIsValidJSON(t *testing.T) {
	raw := atlas.RawJSON()
	if len(raw) == 0 {
		t.Fatal("RawJSON() returned empty slice")
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("RawJSON() is not valid JSON: %v", err)
	}
}

func TestLoadOverallKindScoresInRange(t *testing.T) {
	cards, err := atlas.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	for kind, s := range cards.Overall {
		if s.TP < 0 {
			t.Errorf("kind %s: tp=%d < 0", kind, s.TP)
		}
		if s.FP < 0 {
			t.Errorf("kind %s: fp=%d < 0", kind, s.FP)
		}
		if s.FN < 0 {
			t.Errorf("kind %s: fn=%d < 0", kind, s.FN)
		}
		if s.Precision != nil && (*s.Precision < 0.0 || *s.Precision > 1.0) {
			t.Errorf("kind %s: precision=%f outside [0,1]", kind, *s.Precision)
		}
		if s.Recall != nil && (*s.Recall < 0.0 || *s.Recall > 1.0) {
			t.Errorf("kind %s: recall=%f outside [0,1]", kind, *s.Recall)
		}
		if s.LabelCount < 0 {
			t.Errorf("kind %s: label_count=%d < 0", kind, s.LabelCount)
		}
		if len(s.PrecisionCI95) > 0 && len(s.PrecisionCI95) != 2 {
			t.Errorf("kind %s: precision_ci_95 has wrong length %d", kind, len(s.PrecisionCI95))
		}
		if len(s.RecallCI95) > 0 && len(s.RecallCI95) != 2 {
			t.Errorf("kind %s: recall_ci_95 has wrong length %d", kind, len(s.RecallCI95))
		}
		// CI must not invert
		if len(s.PrecisionCI95) == 2 && s.PrecisionCI95[0] > s.PrecisionCI95[1] {
			t.Errorf("kind %s: precision CI inverted [%f, %f]", kind, s.PrecisionCI95[0], s.PrecisionCI95[1])
		}
		if len(s.RecallCI95) == 2 && s.RecallCI95[0] > s.RecallCI95[1] {
			t.Errorf("kind %s: recall CI inverted [%f, %f]", kind, s.RecallCI95[0], s.RecallCI95[1])
		}
	}
}

func TestLoadPerRepoConsistency(t *testing.T) {
	cards, err := atlas.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	for _, repo := range cards.PerRepo {
		if repo.RepoID == "" {
			t.Error("per_repo entry has empty repo_id")
		}
		if repo.Commit == "" {
			t.Error("per_repo entry has empty commit")
		}
		for kind, s := range repo.Scores {
			if s.Precision != nil && (*s.Precision < 0.0 || *s.Precision > 1.0) {
				t.Errorf("%s kind %s: precision=%f outside [0,1]", repo.RepoID, kind, *s.Precision)
			}
			if s.Recall != nil && (*s.Recall < 0.0 || *s.Recall > 1.0) {
				t.Errorf("%s kind %s: recall=%f outside [0,1]", repo.RepoID, kind, *s.Recall)
			}
		}
	}
}
