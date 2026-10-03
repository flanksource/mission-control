## Guidelines for writing Specs

Specs live in `specs/`. Design docs live in `specs/**/design/`.

- Spec tells how a system should behave. It's the authoritative place to put all the business logic along with the reasoning behind the choices made.
- **Design docs say how a spec is met.** Open with a short Background framing the choice ("X can be done by A or B; we chose B because…"). Then describe the design, then other options considered and why they lost.
  They may name tables and components, but still describe the desired state, not a migration.
- It should not describe why a system moved from one approach to another. Think of it like GitOps. It tells what to become, not what it was or how it should become.
- It should be consistent on its own and avoid involving source code at all
- It should should aim to be concise. One of the goals is to make it human-readable. That means keep it short and approachable.
- It should avoid ambiguity and undefined behaviours as much as possible by addressing them explicitly.
- **Explain every decision**, with a short **Why** directly under the rule it justifies.
- Use MUST, MUST NOT and MAY for requirements.
