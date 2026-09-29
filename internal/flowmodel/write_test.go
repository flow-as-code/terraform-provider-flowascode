// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowmodel

import (
	"math/big"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// Positions round the way the TypeScript writer's Math.round does.
func TestJSRoundIsMathRound(t *testing.T) {
	for v, want := range map[float64]float64{
		-40.5: -40, -7.5: -7, 40.5: 41, 2.4: 2, -2.6: -3, 0.49999999999999994: 0, -0.5: 0,
	} {
		if got := JSRound(v); got != want {
			t.Errorf("JSRound(%v) = %v, want %v", v, got, want)
		}
	}
}

func TestActionsFromDocRoundsANegativeHalfUp(t *testing.T) {
	v, err := jsonv.Decode([]byte(`{"content":{"Version":"2019-10-30","StartAction":"a","Actions":[{"Identifier":"a","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}]},"layout":{"a":{"x":-40.5,"y":-7.5}}}`))
	if err != nil {
		t.Fatal(err)
	}
	actions := ActionsFromDoc(v.(jsonv.Object))
	pos, _ := actions[0].(map[string]any)["position"].(map[string]any)
	x, _ := pos["x"].(*big.Float).Float64()
	y, _ := pos["y"].(*big.Float).Float64()
	if x != -40 || y != -7 {
		t.Fatalf("position (%v, %v), want (-40, -7)", x, y)
	}
}
