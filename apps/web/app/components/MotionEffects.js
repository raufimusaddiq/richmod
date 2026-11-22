"use client";

import { useEffect } from "react";
import { attachMotion } from "../lib/motion";

export default function MotionEffects() {
  useEffect(() => attachMotion(), []);
  return null;
}
