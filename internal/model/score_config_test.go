package model

import (
	"crypto/rand"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	corecrypto "github.com/libp2p/go-libp2p/core/crypto"
	corepeer "github.com/libp2p/go-libp2p/core/peer"
	"gopkg.in/yaml.v3"
)

func scoreTestPeerID(t *testing.T) string {
	t.Helper()
	_, public, err := corecrypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, err := corepeer.IDFromPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

func TestScoreSelectiveDefaultsAndExplicitUnsafeDurations(t *testing.T) {
	original := PeerScoreConfig{Enabled: boolPointer(true), SkipAtomicValidation: true, Topics: map[string]TopicScoreConfig{
		"score/topic": {SkipAtomicValidation: true, TopicWeight: 1},
	}}
	normalized := original.WithDefaults()
	if normalized.DecayInterval != "1s" || normalized.DecayToZero != 0.01 || normalized.Topics["score/topic"].TimeInMeshQuantum != "1s" {
		t.Fatalf("unsafe selective defaults: %+v", normalized)
	}
	if original.DecayInterval != "" || original.Topics["score/topic"].TimeInMeshQuantum != "" {
		t.Fatal("normalizing a score mutated the source configuration")
	}
	if err := original.Validate(); err != nil {
		t.Fatalf("omitted selective score groups: %v", err)
	}
	for _, duration := range []string{"0s", "-1s"} {
		t.Run(duration, func(t *testing.T) {
			score := original.WithDefaults()
			score.DecayInterval = duration
			if err := score.Validate(); err == nil || !strings.Contains(err.Error(), "decayInterval") {
				t.Fatalf("unsafe score ticker accepted: %v", err)
			}
			score = original.WithDefaults()
			topic := score.Topics["score/topic"]
			topic.TimeInMeshQuantum = duration
			score.Topics["score/topic"] = topic
			if err := score.Validate(); err == nil || !strings.Contains(err.Error(), "timeInMeshQuantum") {
				t.Fatalf("unsafe mesh divisor accepted: %v", err)
			}
		})
	}
	if err := (PeerScoreConfig{Enabled: boolPointer(true)}).Validate(); err == nil {
		t.Fatal("atomic validation accepted omitted required parameters")
	}
}

func TestScoreRejectsNonFiniteDisabledParameters(t *testing.T) {
	for _, invalid := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, kind := range []string{"peer", "topic", "threshold"} {
			score := PeerScoreConfig{Enabled: boolPointer(true), SkipAtomicValidation: true}.WithDefaults()
			var target any
			switch kind {
			case "peer":
				target = &score
			case "topic":
				target = &TopicScoreConfig{SkipAtomicValidation: true, TimeInMeshQuantum: "1s"}
			case "threshold":
				target = &PeerScoreThresholdsConfig{SkipAtomicValidation: true}
			}
			value := reflect.ValueOf(target).Elem()
			for index := 0; index < value.NumField(); index++ {
				if value.Field(index).Kind() != reflect.Float64 {
					continue
				}
				fieldName := value.Type().Field(index).Name
				t.Run(kind+"/"+fieldName, func(t *testing.T) {
					previous := value.Field(index).Float()
					value.Field(index).SetFloat(invalid)
					defer value.Field(index).SetFloat(previous)
					candidate := score
					switch item := target.(type) {
					case *PeerScoreConfig:
						candidate = *item
					case *TopicScoreConfig:
						candidate.Topics = map[string]TopicScoreConfig{"score/topic": *item}
					case *PeerScoreThresholdsConfig:
						candidate.Thresholds = *item
					}
					if err := candidate.Validate(); err == nil {
						t.Fatalf("non-finite %s accepted while weights were disabled", fieldName)
					}
				})
			}
		}
	}
}

func TestApplicationScorePolicySerializationAndValidation(t *testing.T) {
	id := scoreTestPeerID(t)
	input := "enabled: true\nskipAtomicValidation: true\nappSpecificWeight: -2\nappSpecificScore:\n  default: 3\n  peers:\n    " + id + ": 0\n"
	var score PeerScoreConfig
	if err := yaml.Unmarshal([]byte(input), &score); err != nil {
		t.Fatal(err)
	}
	if err := score.Validate(); err != nil {
		t.Fatalf("declarative P5 rejected: %v", err)
	}
	data, err := json.Marshal(score)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip PeerScoreConfig
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(score, roundTrip) {
		t.Fatalf("P5 changed in configuration transport: %s", data)
	}
	copied := score.WithDefaults()
	copied.AppSpecificScore.Peers[id] = 7
	copied.AppSpecificScore.Default = 9
	if score.AppSpecificScore.Default != 3 || score.AppSpecificScore.Peers[id] != 0 {
		t.Fatal("normalization shares mutable policy storage")
	}
	invalid := []AppSpecificScoreConfig{
		{Default: math.NaN()},
		{Peers: map[string]float64{"not-a-peer-id": 1}},
		{Peers: map[string]float64{id: math.Inf(1)}},
		{Default: math.MaxFloat64},
	}
	for _, policy := range invalid {
		candidate := score
		candidate.AppSpecificScore = &policy
		if err := candidate.Validate(); err == nil {
			t.Fatalf("invalid application score accepted: %+v", policy)
		}
	}
}

func TestScoreOverlayReplacesWholePolicyAndPreservesZeroValues(t *testing.T) {
	original := NodeConfig{GossipSub: GossipSubConfig{Score: &PeerScoreConfig{Enabled: boolPointer(true),
		SkipAtomicValidation: true, TopicScoreCap: 10,
		AppSpecificWeight: 2, AppSpecificScore: &AppSpecificScoreConfig{Default: 3},
	}}}
	overlay := &PeerScoreConfig{Enabled: boolPointer(false), SkipAtomicValidation: false, TopicScoreCap: 0, AppSpecificWeight: 0}
	merged := original.Merge(NodeConfig{GossipSub: GossipSubConfig{Score: overlay}})
	if merged.GossipSub.Score.IsEnabled() || merged.GossipSub.Score.SkipAtomicValidation || merged.GossipSub.Score.TopicScoreCap != 0 || merged.GossipSub.Score.AppSpecificWeight != 0 || merged.GossipSub.Score.AppSpecificScore != nil {
		t.Fatalf("score replacement lost explicit false/zero or retained an omitted policy: %+v", merged.GossipSub.Score)
	}
}

func TestScoreDisabledTuningDoesNotRequireActiveParameterGroups(t *testing.T) {
	for _, enabled := range []*bool{nil, boolPointer(false)} {
		score := PeerScoreConfig{Enabled: enabled, AppSpecificWeight: 2, Topics: map[string]TopicScoreConfig{
			"score/topic": {TimeInMeshWeight: 3},
		}}
		if score.IsEnabled() {
			t.Fatal("score activated without enabled:true")
		}
		if err := score.Validate(); err != nil {
			t.Fatalf("disabled tuning rejected: %v", err)
		}
		defaults := score.WithDefaults()
		if defaults.DecayInterval != "" || defaults.Topics["score/topic"].TimeInMeshQuantum != "" {
			t.Fatal("disabled score received active runtime defaults")
		}
		score.DecayInterval = "not-a-duration"
		if err := score.Validate(); err == nil {
			t.Fatal("disabled score accepted malformed duration")
		}
		score.DecayInterval = ""
		score.BehaviourPenaltyDecay = math.NaN()
		if err := score.Validate(); err == nil {
			t.Fatal("disabled score accepted non-finite data")
		}
	}
}
