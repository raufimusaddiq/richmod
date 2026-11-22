import { useEffect } from "react";
import AuthProvider from "./components/AuthProvider";
import CategoryPaletteProvider from "./components/CategoryPaletteProvider";
import InboxCountProvider from "./components/InboxCountProvider";
import KeyboardViewport from "./components/KeyboardViewport";
import MotionEffects from "./components/MotionEffects";
import QuickRecordProvider from "./components/QuickRecordProvider";
import ServiceWorkerRegistrar from "./components/ServiceWorkerRegistrar";
import { RouterProvider, useDocumentMeta, usePathname, useRouter } from "./router";
import { matchRoute } from "./routes";

function Redirect({ to }) {
  const router = useRouter();
  useEffect(() => { router.replace(to); }, [router, to]);
  return null;
}

function RouteView() {
  const pathname = usePathname();
  const route = matchRoute(pathname);
  useDocumentMeta(route.title, route.description);
  if (route.redirect) return <Redirect to={route.redirect} />;
  const Page = route.component;
  return <div className="route-fade" key={pathname}><Page /></div>;
}

export default function App({ initial }) {
  return <RouterProvider initial={initial}>
    <ServiceWorkerRegistrar />
    <KeyboardViewport />
    <MotionEffects />
    <AuthProvider><CategoryPaletteProvider><InboxCountProvider><QuickRecordProvider>
      <RouteView />
    </QuickRecordProvider></InboxCountProvider></CategoryPaletteProvider></AuthProvider>
  </RouterProvider>;
}
