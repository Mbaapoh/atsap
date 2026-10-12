import { setupServer } from "msw/node";

// Component tests intercept HTTP in Node (MSW's server-side interception,
// not a browser service worker). Unhandled requests fail the test, so no
// test can quietly reach a real network.
export const server = setupServer();
