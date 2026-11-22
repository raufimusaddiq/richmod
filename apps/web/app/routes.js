import Home from "./page";
import Transactions from "./transactions/page";
import Analytics from "./analytics/page";
import Documents from "./documents/page";
import Household from "./household/page";
import Settings from "./settings/page";
import Wealth from "./wealth/page";
import Inbox from "./inbox/page";
import Admin from "./admin/page";
import Login from "./login/page";
import Invite from "./invite/page";
import Privacy from "./privacy/page";
import Terms from "./terms/page";
import NotFound from "./not-found";
import { metaFor, notFoundMeta, routeMeta } from "./lib/siteMeta";

const components = {
  "/": Home,
  "/transactions": Transactions,
  "/analytics": Analytics,
  "/documents": Documents,
  "/household": Household,
  "/settings": Settings,
  "/wealth": Wealth,
  "/inbox": Inbox,
  "/admin": Admin,
  "/login": Login,
  "/invite": Invite,
  "/privacy": Privacy,
  "/terms": Terms,
};

export function matchRoute(pathname) {
  const meta = metaFor(pathname);
  if (!meta) return { path: pathname, component: NotFound, ...notFoundMeta };
  return { path: pathname, component: components[pathname] ?? null, ...meta };
}

export const routePaths = Object.keys(routeMeta);
