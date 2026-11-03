// Remounts on every navigation, which is the one moment a route-level entry
// animation can play. It only wraps the page body in styles/15-motion.css.
export default function Template({ children }) {
  return <div className="route-fade">{children}</div>;
}
