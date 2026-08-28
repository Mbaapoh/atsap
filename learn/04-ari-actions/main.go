// Stage 4: ARI actions (answer/hangup/play) driven from os.Args.
// See README.md in this folder.
package main

func main() {
	// TODO:
	// 1. Write answerChannel(id string) error -> POST /channels/{id}/answer
	// 2. Write hangupChannel(id, reason string) error -> DELETE /channels/{id}?reason=...
	// 3. Read os.Args to pick which action to run and against which channel ID.
	// 4. Call it, print the result or error.
}
