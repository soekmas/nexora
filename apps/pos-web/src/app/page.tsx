import React from "react";

export default function HomePage() {
  return (
    <main
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        minHeight: "100vh",
        padding: "2rem",
        textAlign: "center",
      }}
    >
      <div
        style={{
          background: "var(--surface)",
          border: "1px solid var(--border)",
          borderRadius: "8px",
          padding: "2rem",
          maxWidth: "480px",
          width: "100%",
        }}
      >
        <h1 style={{ fontSize: "1.5rem", marginBottom: "0.75rem" }}>
          NEXORA POS
        </h1>
        <p style={{ color: "var(--muted)", fontSize: "0.875rem" }}>
          Retail Business Platform — Terminal Shell
        </p>
        <div
          style={{
            marginTop: "1.5rem",
            padding: "0.75rem",
            background: "var(--background)",
            borderRadius: "4px",
            fontSize: "0.75rem",
            color: "var(--muted)",
          }}
        >
          Phase 0 Bootstrap Initialized
        </div>
      </div>
    </main>
  );
}
