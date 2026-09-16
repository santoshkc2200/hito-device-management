import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import App from "./App";

describe("staff app shell", () => {
  it("renders the app title from the catalogue, not a literal", async () => {
    render(<App />);
    expect(await screen.findByText("HDMS")).toBeInTheDocument();
  });
});
