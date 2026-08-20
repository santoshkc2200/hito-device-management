import { resolve } from "node:path";

/**
 * Returns Chromium launch args for using a synthetic video/camera stream during tests.
 * @param y4mFilePath Optional path to a .y4m video capture file.
 */
export function getFakeCameraLaunchArgs(y4mFilePath?: string): string[] {
  const args = ["--use-fake-device-for-media-stream"];
  if (y4mFilePath) {
    args.push(`--use-file-for-fake-video-capture=${resolve(y4mFilePath)}`);
  }
  return args;
}
