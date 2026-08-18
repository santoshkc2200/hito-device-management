import { defineConfig } from "@hey-api/openapi-ts";

// Generates a typed client + Zod schemas from the same OpenAPI spec
// oapi-codegen reads for the Go server (docs/06-api-contract.md). Neither
// side is hand-edited; `task generate` regenerates both together.
export default defineConfig({
  input: "../../../hdms-backend/api/openapi.yaml",
  output: "src/gen",
  plugins: [
    "@hey-api/client-fetch",
    "@hey-api/typescript",
    "@hey-api/sdk",
    "zod",
  ],
});
