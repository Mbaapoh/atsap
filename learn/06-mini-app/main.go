// Stage 6: your own mini VoIP app — AMI events + ARI events running
// concurrently, auto-answering inbound calls. See README.md in this folder.
package main

func main() {
	// TODO:
	// 1. context.Background() + signal.NotifyContext for graceful shutdown.
	// 2. Connect AMI (stage 1/2), launch its read loop in a goroutine.
	// 3. Dial the ARI events websocket (stage 5), launch its read loop in
	//    a goroutine; on "StasisStart", call answerChannel (stage 4) with
	//    the channel ID from the event payload.
	// 4. Block on <-ctx.Done(), then close both connections.
}
