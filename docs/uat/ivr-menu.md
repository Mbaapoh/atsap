# UAT — IVR menu (D-90)

1. Create two extensions and a flow on a spare DID with prompt
   `sound:demo-congrats`, menu `1=<first>`, `2=<second>`, timeout 5,
   no-input and invalid to the first extension. Publish.
2. Call the DID from a desk phone. Expect the prompt, then press `1`;
   the first extension rings and answers with audio both ways.
3. Repeat and press `2`; the second extension rings.
4. Press no digit; after the timeout the no-input destination rings.
5. Press `9`; the invalid destination rings.
6. Set the flow closed for now (or test on a holiday); the closed
   destination rings and no prompt is played.
7. Roll back to the previous revision during a new call; the live call
   keeps its menu, the next call uses the old revision.
8. A playback failure (remove the sound file) ends the call with a stable
   reason and never rings a random extension.
