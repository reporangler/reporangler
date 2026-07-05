-- Supporting indexes for the user/permission endpoints added in the Go port.
-- Idempotent (IF NOT EXISTS) so this runs safely on every boot.
--
-- idx_capability_map_capability accelerates admin-counting and the
-- capability-name joins used by grant/revoke and package-group permissions.
-- idx_capability_map_constraint is a GIN index over the jsonb constraint so the
-- PACKAGE_GROUP_ACCESS containment (@>) lookups (join/leave/protect) are indexed.

CREATE INDEX IF NOT EXISTS idx_capability_map_capability
  ON capability_map (capability_id);

CREATE INDEX IF NOT EXISTS idx_capability_map_constraint
  ON capability_map USING gin ("constraint");
