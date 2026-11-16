package valueobject

import (
	"encoding/json"
	"testing"
)

func TestRialJSONRoundTrip(t *testing.T) {
	for _, value := range []int64{0, 1, 1890000000, 9007199254740991} {
		original := FromRials(value)
		raw, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var got Money
		if err := json.Unmarshal(raw, &got); err != nil || got.ToRials() != value {
			t.Fatalf("%s: %v %+v", raw, err, got)
		}
	}
	if NewMoney(100).ToRials() != 100 {
		t.Fatal("NewMoney changed rial input")
	}
	for _, raw := range []string{"null", "1.1", "9007199254740992", "\"100\""} {
		var m Money
		if json.Unmarshal([]byte(raw), &m) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
