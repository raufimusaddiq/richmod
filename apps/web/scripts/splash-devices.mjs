// One entry per distinct portrait screen geometry among installed iPhones.
// Logical size and scale factor come from ios-resolution.com (checked
// 2026-11-03); the pixel size iOS actually draws is logical × scale. The media
// query must carry the logical size *and* the pixel ratio, because several
// devices share a logical size at different densities (414×896 exists at both
// 2× and 3×).
//
// 375×812 @3 is the one place the table disagrees with itself: the X/XS/11 Pro
// are 1125×2436 and the 12/13 mini are 1080×2340 at the same logical size, so
// only one image can match. The larger 1125×2436 is used; the mini scales it
// down by 4%.
//
// iPads and landscape are not covered: the installed app is a phone-portrait
// layout, and landscape launch images would double the bytes for a mode this
// UI does not target.
export const splashDevices = [
  { width: 440, height: 956, ratio: 3, models: "iPhone 16/17/18 Pro Max" },
  { width: 430, height: 932, ratio: 3, models: "iPhone 14 Pro Max, 15 Plus, 15 Pro Max, 16 Plus" },
  { width: 428, height: 926, ratio: 3, models: "iPhone 12/13 Pro Max, 14 Plus" },
  { width: 420, height: 912, ratio: 3, models: "iPhone Air" },
  { width: 414, height: 896, ratio: 3, models: "iPhone XS Max, 11 Pro Max" },
  { width: 414, height: 896, ratio: 2, models: "iPhone XR, 11" },
  { width: 402, height: 874, ratio: 3, models: "iPhone 16 Pro, 17, 17 Pro, 18 Pro" },
  { width: 393, height: 852, ratio: 3, models: "iPhone 14 Pro, 15, 15 Pro, 16" },
  { width: 390, height: 844, ratio: 3, models: "iPhone 12/13/14, 13 Pro, 16e, 17e" },
  { width: 375, height: 812, ratio: 3, models: "iPhone X, XS, 11 Pro (and 12/13 mini, scaled down)" },
  { width: 375, height: 667, ratio: 2, models: "iPhone SE 2nd/3rd gen, 8, 7, 6s" },
  { width: 320, height: 568, ratio: 2, models: "iPhone SE 1st gen, 5s" },
].map(device => ({
  ...device,
  pixelWidth: device.width * device.ratio,
  pixelHeight: device.height * device.ratio,
  file: `apple-splash-${device.width * device.ratio}x${device.height * device.ratio}.png`,
  media: `(device-width: ${device.width}px) and (device-height: ${device.height}px) and (-webkit-device-pixel-ratio: ${device.ratio}) and (orientation: portrait)`,
}));
