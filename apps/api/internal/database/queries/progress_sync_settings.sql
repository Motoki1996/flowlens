-- Progress sync on issue close settings (issue #202). No owner column of
-- its own; the owner-scoped query joins through project_members the same
-- way notification_settings does. The sync paths (webhookapply,
-- projectsync) have no acting user, so IsProgressSyncEnabledForProject is
-- unscoped, like ListEnabledNotificationSettings.

-- name: UpsertProgressSyncSettings :one
INSERT INTO progress_sync_settings (project_id, enabled)
VALUES ($1, $2)
ON CONFLICT (project_id) DO UPDATE
SET enabled = EXCLUDED.enabled,
    updated_at = now()
RETURNING *;

-- name: GetProgressSyncSettingsForOwner :one
SELECT pss.*
FROM progress_sync_settings pss
WHERE pss.project_id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = pss.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role = 'owner'
  );

-- IsProgressSyncEnabledForProject reports false, not an error, for a
-- project that has never saved a row: settings conceptually always exist,
-- just possibly unset (default off).

-- name: IsProgressSyncEnabledForProject :one
SELECT COALESCE((SELECT enabled FROM progress_sync_settings WHERE project_id = $1), false)::bool AS enabled;
