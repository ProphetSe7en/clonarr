// Package agents implements a provider-based notification system for Clonarr.
//
// The architecture follows a registry pattern: each notification provider
// (Discord, Gotify, Pushover, etc.) implements the [Provider] interface and
// self-registers via its init() function. This decouples provider-specific
// logic from the core application, making it straightforward to add new
// providers without modifying existing code.
//
// # Key concepts
//
//   - [Agent] — one user-configured notification provider instance (e.g.
//     "Discord #alerts"). Stored in the config's NotificationAgents slice.
//   - [Provider] — the implementation contract for a notification backend.
//     Handles validation, credential masking, testing, and message delivery.
//   - [Payload] — the provider-agnostic message passed to [DispatchAgent].
//     Contains title, body, severity, routing, and optional per-provider
//     message overrides.
//   - [Runtime] — process-scoped dependencies (HTTP clients, app version)
//     injected at dispatch time. Providers never construct their own clients.
//
// # Adding a new provider
//
// To add a new notification provider:
//  1. Create a new file (e.g. slack.go) in this package.
//  2. Define an unexported struct implementing [Provider].
//  3. Add an init() function that calls registerProvider with an instance.
//  4. Extend [Config] with any provider-specific credential fields.
//  5. Write tests covering Validate, MaskConfig, PreserveConfig, and
//     priority/routing logic.
//
// The provider is automatically available to the rest of the application
// through the registry — no wiring changes needed in core or API packages.
//
// # Credential security
//
// Providers must implement MaskConfig and PreserveConfig to ensure that
// raw secrets (webhook URLs, API tokens) are never returned to the UI.
// The mask/preserve round-trip allows the frontend to display placeholder
// values and submit them back without losing the stored credential.
package agents
