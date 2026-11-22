"use client";

import { useEffect } from "react";
import { attachMotion } from "../lib/interactionMotion";

export default function MotionEffects() {
  useEffect(() => attachMotion(), []);
  return null;
}
