# No authentication, no user identity

The system enforces no authentication and tracks no user identity. Anyone who can reach the deployment may upload, query, cancel any Query Job, view or download any Query Result, and trigger any administrative action. Query Jobs carry no owner — they are identified by id only.

This is a deliberate choice to keep the system simple for a small, trusted user base. The Queue is global FIFO with no per-user fairness or ownership, cancellation is global, and the Status Board shows every job to every visitor.

The load-bearing consequence: because the application itself enforces zero access control, **the deployment must be network-isolated to a trusted environment** (private LAN, VPN, internal research subnet). Exposing the system beyond a trusted network would let anyone dump the Dataset or saturate the Queue. Network-level isolation is the sole access boundary and must not be removed without reintroducing application-level auth. The cost of this decision is borne entirely by the deployment topology, not the code.
