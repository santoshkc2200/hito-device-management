import type { SoundId } from "./feedback-config";

let audioCtx: AudioContext | null = null;
let isUnlocked = false;

export function getAudioContext(): AudioContext | null {
  if (typeof window === "undefined") return null;

  if (!audioCtx) {
    const AudioContextClass =
      window.AudioContext ||
      (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (AudioContextClass) {
      try {
        audioCtx = new AudioContextClass();
      } catch {
        audioCtx = null;
      }
    }
  }
  return audioCtx;
}

export function setAudioContextForTest(ctx: AudioContext | null): void {
  audioCtx = ctx;
  isUnlocked = false;
}

export function unlockAudio(): void {
  if (isUnlocked) return;
  const ctx = getAudioContext();
  if (!ctx) return;

  if (ctx.state === "suspended") {
    ctx.resume().catch(() => {
      // Silent error ignore
    });
  }
  isUnlocked = true;
}

// Automatically register unlock listeners in browser environment
if (typeof window !== "undefined") {
  const handleInteraction = () => {
    unlockAudio();
  };

  window.addEventListener("pointerdown", handleInteraction, { capture: true, passive: true });
  window.addEventListener("keydown", handleInteraction, { capture: true, passive: true });
  window.addEventListener("touchstart", handleInteraction, { capture: true, passive: true });
}

function playRisingChime(ctx: AudioContext): void {
  const now = ctx.currentTime;
  const gainNode = ctx.createGain();
  gainNode.gain.setValueAtTime(0.001, now);
  gainNode.gain.exponentialRampToValueAtTime(0.15, now + 0.02);
  gainNode.gain.setValueAtTime(0.15, now + 0.08);
  gainNode.gain.exponentialRampToValueAtTime(0.2, now + 0.1);
  gainNode.gain.exponentialRampToValueAtTime(0.001, now + 0.22);
  gainNode.connect(ctx.destination);

  const osc = ctx.createOscillator();
  osc.type = "sine";
  osc.frequency.setValueAtTime(587.33, now); // D5
  osc.frequency.setValueAtTime(880.0, now + 0.08); // A5
  osc.connect(gainNode);

  osc.start(now);
  osc.stop(now + 0.24);
}

function playFallingChime(ctx: AudioContext): void {
  const now = ctx.currentTime;
  const gainNode = ctx.createGain();
  gainNode.gain.setValueAtTime(0.001, now);
  gainNode.gain.exponentialRampToValueAtTime(0.15, now + 0.02);
  gainNode.gain.setValueAtTime(0.15, now + 0.08);
  gainNode.gain.exponentialRampToValueAtTime(0.2, now + 0.1);
  gainNode.gain.exponentialRampToValueAtTime(0.001, now + 0.22);
  gainNode.connect(ctx.destination);

  const osc = ctx.createOscillator();
  osc.type = "sine";
  osc.frequency.setValueAtTime(880.0, now); // A5
  osc.frequency.setValueAtTime(587.33, now + 0.08); // D5
  osc.connect(gainNode);

  osc.start(now);
  osc.stop(now + 0.24);
}

function playLowBuzz(ctx: AudioContext): void {
  const now = ctx.currentTime;
  const gainNode = ctx.createGain();
  gainNode.gain.setValueAtTime(0.001, now);
  gainNode.gain.exponentialRampToValueAtTime(0.18, now + 0.03);
  gainNode.gain.exponentialRampToValueAtTime(0.001, now + 0.2);
  gainNode.connect(ctx.destination);

  const filter = ctx.createBiquadFilter();
  filter.type = "lowpass";
  filter.frequency.setValueAtTime(350, now);
  filter.connect(gainNode);

  const osc = ctx.createOscillator();
  osc.type = "sawtooth";
  osc.frequency.setValueAtTime(150.0, now);
  osc.connect(filter);

  osc.start(now);
  osc.stop(now + 0.22);
}

function playSoftClick(ctx: AudioContext): void {
  const now = ctx.currentTime;
  const gainNode = ctx.createGain();
  gainNode.gain.setValueAtTime(0.001, now);
  gainNode.gain.exponentialRampToValueAtTime(0.08, now + 0.005);
  gainNode.gain.exponentialRampToValueAtTime(0.001, now + 0.06);
  gainNode.connect(ctx.destination);

  const osc = ctx.createOscillator();
  osc.type = "sine";
  osc.frequency.setValueAtTime(1200, now);
  osc.frequency.exponentialRampToValueAtTime(200, now + 0.05);
  osc.connect(gainNode);

  osc.start(now);
  osc.stop(now + 0.07);
}

export function playFeedbackSound(soundId: SoundId, isMuted = false): void {
  if (isMuted || soundId === "none") {
    return;
  }

  try {
    const ctx = getAudioContext();
    if (!ctx) return;

    if (ctx.state === "suspended") {
      ctx.resume().catch(() => {
        // Silent ignore
      });
    }

    switch (soundId) {
      case "borrow":
        playRisingChime(ctx);
        break;
      case "return":
        playFallingChime(ctx);
        break;
      case "reject":
        playLowBuzz(ctx);
        break;
      case "accepted":
        playSoftClick(ctx);
        break;
    }
  } catch {
    // Failure is completely silent — no errors surfaced, no transitions blocked
  }
}
