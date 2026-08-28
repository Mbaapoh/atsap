// Stage 5: connect to the ARI event WebSocket and print each event's type.
// See README.md in this folder.
package main

func main() {
	// TODO:
	// 1. Build ws://.../ari/events?app=...&api_key=user:pass&subscribeAll=true
	//    from ARI_URL, ARI_APP_NAME, ARI_USERNAME, ARI_PASSWORD.
	// 2. websocket.DefaultDialer.Dial(url, nil) — see github.com/gorilla/websocket.
	// 3. Loop: conn.ReadMessage(), unmarshal into a struct with a Type field,
	//    print the type and the raw payload.
}
