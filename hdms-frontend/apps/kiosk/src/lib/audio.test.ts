import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  playFeedbackSound,
  unlockAudio,
  setAudioContextForTest,
} from "./audio";

describe("Web Audio Feedback System", () => {
  let mockCtx: any;
  let mockGain: any;
  let mockOsc: any;
  let mockFilter: any;

  beforeEach(() => {
    mockGain = {
      gain: {
        setValueAtTime: vi.fn(),
        exponentialRampToValueAtTime: vi.fn(),
      },
      connect: vi.fn(),
    };

    mockOsc = {
      type: "sine",
      frequency: {
        setValueAtTime: vi.fn(),
        exponentialRampToValueAtTime: vi.fn(),
      },
      connect: vi.fn(),
      start: vi.fn(),
      stop: vi.fn(),
    };

    mockFilter = {
      type: "lowpass",
      frequency: {
        setValueAtTime: vi.fn(),
      },
      connect: vi.fn(),
    };

    mockCtx = {
      state: "running",
      currentTime: 100,
      destination: {},
      createGain: vi.fn(() => mockGain),
      createOscillator: vi.fn(() => mockOsc),
      createBiquadFilter: vi.fn(() => mockFilter),
      resume: vi.fn().mockResolvedValue(undefined),
    };

    setAudioContextForTest(mockCtx);
  });

  afterEach(() => {
    setAudioContextForTest(null);
    vi.restoreAllMocks();
  });

  it("mutedKioskPlaysNothing", () => {
    playFeedbackSound("borrow", true);
    expect(mockCtx.createOscillator).not.toHaveBeenCalled();
    expect(mockCtx.createGain).not.toHaveBeenCalled();

    playFeedbackSound("return", true);
    expect(mockCtx.createOscillator).not.toHaveBeenCalled();

    playFeedbackSound("reject", true);
    expect(mockCtx.createOscillator).not.toHaveBeenCalled();

    playFeedbackSound("accepted", true);
    expect(mockCtx.createOscillator).not.toHaveBeenCalled();
  });

  it("playsNoneProducesNoAudio", () => {
    playFeedbackSound("none", false);
    expect(mockCtx.createOscillator).not.toHaveBeenCalled();
  });

  it("audioFailureDoesNotThrowOrBlockTransition", () => {
    mockCtx.createGain.mockImplementation(() => {
      throw new Error("WebAudio context failed or hardware busy");
    });

    expect(() => {
      playFeedbackSound("borrow", false);
      playFeedbackSound("return", false);
      playFeedbackSound("reject", false);
      playFeedbackSound("accepted", false);
    }).not.toThrow();
  });

  it("unlockOnFirstGesture resumes suspended context", async () => {
    mockCtx.state = "suspended";
    unlockAudio();
    expect(mockCtx.resume).toHaveBeenCalled();
  });

  it("playsSynthesizedSoundForBorrow", () => {
    playFeedbackSound("borrow", false);
    expect(mockCtx.createGain).toHaveBeenCalled();
    expect(mockCtx.createOscillator).toHaveBeenCalled();
    expect(mockOsc.start).toHaveBeenCalledWith(100);
    expect(mockOsc.stop).toHaveBeenCalled();
  });

  it("playsSynthesizedSoundForReturn", () => {
    playFeedbackSound("return", false);
    expect(mockCtx.createGain).toHaveBeenCalled();
    expect(mockCtx.createOscillator).toHaveBeenCalled();
    expect(mockOsc.start).toHaveBeenCalledWith(100);
    expect(mockOsc.stop).toHaveBeenCalled();
  });

  it("playsSynthesizedSoundForReject", () => {
    playFeedbackSound("reject", false);
    expect(mockCtx.createGain).toHaveBeenCalled();
    expect(mockCtx.createBiquadFilter).toHaveBeenCalled();
    expect(mockCtx.createOscillator).toHaveBeenCalled();
    expect(mockOsc.start).toHaveBeenCalledWith(100);
    expect(mockOsc.stop).toHaveBeenCalled();
  });

  it("playsSynthesizedSoundForAccepted", () => {
    playFeedbackSound("accepted", false);
    expect(mockCtx.createGain).toHaveBeenCalled();
    expect(mockCtx.createOscillator).toHaveBeenCalled();
    expect(mockOsc.start).toHaveBeenCalledWith(100);
    expect(mockOsc.stop).toHaveBeenCalled();
  });
});
