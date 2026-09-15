import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import { axe } from "vitest-axe";
import { CameraOverlay } from "./camera-overlay";
import { type CameraSource } from "@hdms/scan";

function createMockCameraSource(overrides?: Partial<CameraSource>): CameraSource {
  const videoEl = document.createElement("video");
  let currentFacingMode: "user" | "environment" = "user";
  return {
    id: "camera",
    label: "Camera Scanner",
    isAvailable: vi.fn().mockResolvedValue(true),
    start: vi.fn().mockImplementation((_emit) => {
      return Promise.resolve();
    }),
    stop: vi.fn().mockResolvedValue(undefined),
    hasTorch: vi.fn().mockReturnValue(true),
    isTorchOn: vi.fn().mockReturnValue(false),
    setTorch: vi.fn().mockResolvedValue(true),
    isActive: vi.fn().mockReturnValue(true),
    getFacingMode: vi.fn().mockImplementation(() => currentFacingMode),
    switchCamera: vi.fn().mockImplementation(async (mode) => {
      currentFacingMode = mode ?? (currentFacingMode === "user" ? "environment" : "user");
      return currentFacingMode;
    }),
    getVideoElement: vi.fn().mockReturnValue(videoEl),
    getStream: vi.fn().mockReturnValue(null),
    getLastError: vi.fn().mockReturnValue(null),
    ...overrides,
  } as unknown as CameraSource;
}

describe("<CameraOverlay />", () => {
  it("renders nothing when isOpen is false", () => {
    const { container } = render(
      <CameraOverlay isOpen={false} onClose={vi.fn()} />
    );
    expect(container.firstChild).toBeNull();
  });

  it("renders viewfinder UI with accessible structure", async () => {
    const mockCamera = createMockCameraSource();
    const onClose = vi.fn();

    const { container } = render(
      <CameraOverlay
        isOpen={true}
        onClose={onClose}
        cameraSource={mockCamera}
      />
    );

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText("Camera Barcode Scanner")).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Rear Camera" })).toBeInTheDocument();
    expect(await screen.findByText("Turn Torch On")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("starts camera on open and stops camera on unmount/close", () => {
    const mockCamera = createMockCameraSource();
    const onClose = vi.fn();

    const { unmount } = render(
      <CameraOverlay
        isOpen={true}
        onClose={onClose}
        cameraSource={mockCamera}
      />
    );

    expect(mockCamera.start).toHaveBeenCalled();

    unmount();
    expect(mockCamera.stop).toHaveBeenCalled();
  });

  it("handles Cancel button click", async () => {
    const user = userEvent.setup();
    const mockCamera = createMockCameraSource();
    const onClose = vi.fn();

    render(
      <CameraOverlay
        isOpen={true}
        onClose={onClose}
        cameraSource={mockCamera}
      />
    );

    const cancelBtn = screen.getByRole("button", { name: "Cancel" });
    await user.click(cancelBtn);

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("toggles torch when torch button is clicked", async () => {
    const user = userEvent.setup();
    const mockCamera = createMockCameraSource();

    render(
      <CameraOverlay
        isOpen={true}
        onClose={vi.fn()}
        cameraSource={mockCamera}
      />
    );

    const torchBtn = await screen.findByRole("button", { name: "Turn Torch On" });
    await user.click(torchBtn);

    expect(mockCamera.setTorch).toHaveBeenCalledWith(true);
  });

  it("displays permission error guidance when camera fails to start", async () => {
    const mockCamera = createMockCameraSource({
      start: vi.fn().mockRejectedValue(new Error("Permission denied")),
    });
    const onManualEntry = vi.fn();

    render(
      <CameraOverlay
        isOpen={true}
        onClose={vi.fn()}
        cameraSource={mockCamera}
        onManualEntry={onManualEntry}
      />
    );

    expect(
      await screen.findByText("Camera Access Blocked")
    ).toBeInTheDocument();
    expect(
      screen.getByText(/check Settings > Safari > Camera permissions/i)
    ).toBeInTheDocument();

    const manualBtn = screen.getByRole("button", {
      name: "Attendant Manual Entry (PIN)",
    });
    await userEvent.click(manualBtn);

    expect(onManualEntry).toHaveBeenCalledTimes(1);
  });

  it("emits scan and closes overlay on successful read", async () => {
    let capturedEmit: ((token: string) => void) | null = null;
    const mockCamera = createMockCameraSource({
      start: vi.fn().mockImplementation((emit) => {
        capturedEmit = emit;
        return Promise.resolve();
      }),
    });

    const onScan = vi.fn();
    const onClose = vi.fn();

    render(
      <CameraOverlay
        isOpen={true}
        onClose={onClose}
        onScan={onScan}
        cameraSource={mockCamera}
      />
    );

    expect(mockCamera.start).toHaveBeenCalled();
    expect(capturedEmit).not.toBeNull();

    // Trigger scan from camera
    capturedEmit!("HD-U-7K3M9QXA2F-4");

    expect(onScan).toHaveBeenCalledWith("HD-U-7K3M9QXA2F-4");
    expect(onClose).toHaveBeenCalled();
  });

  it("switches camera between front and rear when switch button is clicked", async () => {
    const user = userEvent.setup();
    let currentMode: "user" | "environment" = "user";
    const switchCamera = vi.fn().mockImplementation(async () => {
      currentMode = currentMode === "user" ? "environment" : "user";
      return currentMode;
    });

    const mockCamera = createMockCameraSource({
      getFacingMode: () => currentMode,
      switchCamera,
    });

    render(
      <CameraOverlay
        isOpen={true}
        onClose={vi.fn()}
        cameraSource={mockCamera}
      />
    );

    // Initial state: front camera is default, so button offers switching to Rear Camera
    const switchBtn = await screen.findByRole("button", { name: "Rear Camera" });
    expect(switchBtn).toBeInTheDocument();

    // Click switch camera button
    await user.click(switchBtn);

    expect(switchCamera).toHaveBeenCalledTimes(1);
    // After switching to environment, button now displays Front Camera
    expect(await screen.findByRole("button", { name: "Front Camera" })).toBeInTheDocument();

    // Click again to switch back to Front Camera
    await user.click(screen.getByRole("button", { name: "Front Camera" }));
    expect(switchCamera).toHaveBeenCalledTimes(2);
    expect(await screen.findByRole("button", { name: "Rear Camera" })).toBeInTheDocument();
  });
});
