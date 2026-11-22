import "./globals.css";
import "./detail-drawer.css";
import { createRoot, hydrateRoot } from "react-dom/client";
import App from "./App";
import { installHistoryNotifier } from "./router/location";

installHistoryNotifier();

const SUSPENSE_CLIENT_RENDER = /#419|Switched to client rendering/;

function reportRecoverable(error) {
  if (SUSPENSE_CLIENT_RENDER.test(String(error?.message))) return;
  reportError(error);
}

const container = document.getElementById("root");
const element = <App initial={{ pathname: window.location.pathname, search: window.location.search }} />;

if (container.hasChildNodes()) hydrateRoot(container, element, { onRecoverableError: reportRecoverable });
else createRoot(container).render(element);
