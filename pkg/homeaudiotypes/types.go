package homeaudiotypes

type SpeakInput struct {
	Phrase  string   `json:"phrase"`  // phrase to speak out
	Devices []string `json:"devices"` // devices on which to play the phrase
}
