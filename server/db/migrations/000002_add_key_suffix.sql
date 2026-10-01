-- Add key_suffix column to store last 5 characters for display
ALTER TABLE client_api_keys ADD COLUMN key_suffix TEXT NOT NULL DEFAULT '';

-- Remove default after adding column
ALTER TABLE client_api_keys ALTER COLUMN key_suffix DROP DEFAULT;