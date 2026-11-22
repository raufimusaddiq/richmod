import { useEffect } from "react";
import { attachKeyboardViewport } from "../lib/keyboardViewport";

export function useKeyboardViewport() {
  useEffect(() => attachKeyboardViewport(), []);
}

export default function KeyboardViewport() {
  useKeyboardViewport();
  return null;
}
