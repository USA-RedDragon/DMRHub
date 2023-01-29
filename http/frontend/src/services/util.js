export function getWebsocketURI() {
  var loc = window.location;
  var new_uri;
  if (loc.protocol === "https:") {
    new_uri = "wss:";
  } else {
    new_uri = "ws:";
  }
  // nodejs development
  if (window.location.port == 5173) {
    // Change port to 3005
    new_uri += "//" + loc.hostname + ":3005";
  } else {
    new_uri += "//" + loc.host;
  }
  new_uri += "/ws";
  console.log('Websocket URI: "' + new_uri + '"');
  return new_uri;
}
