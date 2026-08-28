// Stage 2: after logging in (stage 1), read the continuous stream of AMI
// events and print them. See README.md in this folder.
package main

func main() {
	// TODO:
	// 1. Connect + log in (same as stage 1).
	// 2. Write a readMessage(r *bufio.Reader) (map[string]string, error)
	//    helper: read lines until a blank line, splitting each on ":".
	// 3. Loop calling readMessage forever, printing whenever the message
	//    has an "Event" key.
}
