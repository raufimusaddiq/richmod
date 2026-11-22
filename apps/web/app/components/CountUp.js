import { useCountUpText } from "../lib/useMotion";

export default function CountUp({ text }) {
  const { animating, visible } = useCountUpText(text);
  if (!animating) return text;
  return <><span className="count-up" aria-hidden="true">{visible}</span><span className="visually-hidden">{text}</span></>;
}
