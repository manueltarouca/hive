// Package names makes friendly session names, like "drifting-cedar":
// an adjective and a noun from nature, kebab-cased, unique among the names
// in use.
package names

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

var adjectives = []string{
	"Amber", "Ancient", "Autumn", "Bold", "Brisk", "Calm", "Clear", "Crisp", "Dawn", "Drifting",
	"Dusky", "Early", "Emerald", "Faded", "Gentle", "Gilded", "Golden", "Hidden", "Hollow", "Icy",
	"Lively", "Lone", "Lucid", "Misty", "Mossy", "Nimble", "Patient", "Quiet", "Rapid", "Restless",
	"Rising", "Rustic", "Silent", "Silver", "Sleepy", "Solar", "Steady", "Still", "Sunny", "Swift",
	"Tidal", "Velvet", "Wandering", "Wild", "Windy", "Winter", "Woven", "Young",
}

var nouns = []string{
	"Aspen", "Bay", "Birch", "Brook", "Canyon", "Cedar", "Cliff", "Cloud", "Comet", "Coral",
	"Cove", "Delta", "Dune", "Ember", "Fern", "Field", "Fjord", "Forest", "Glade", "Glacier",
	"Grove", "Harbor", "Heath", "Hill", "Lagoon", "Lake", "Meadow", "Mesa", "Moon", "Oasis",
	"Orchard", "Pine", "Prairie", "Reef", "Ridge", "River", "Sierra", "Spring", "Summit", "Thicket",
	"Tide", "Trail", "Tundra", "Valley", "Willow", "Wind",
}

// New returns a name that taken reports as free.
func New(taken func(string) bool) string {
	for i := 0; i < 200; i++ {
		n := strings.ToLower(adjectives[rand.IntN(len(adjectives))] + "-" + nouns[rand.IntN(len(nouns))])
		if !taken(n) {
			return n
		}
	}
	for i := 2; ; i++ { // every pair in use: number them
		n := strings.ToLower(fmt.Sprintf("%s-%s-%d", adjectives[rand.IntN(len(adjectives))], nouns[rand.IntN(len(nouns))], i))
		if !taken(n) {
			return n
		}
	}
}
