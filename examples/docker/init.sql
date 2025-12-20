-- Enable pg_kafka extension
CREATE EXTENSION IF NOT EXISTS pg_kafka;

-- Example: Create a test table
CREATE TABLE IF NOT EXISTS events (
    id SERIAL PRIMARY KEY,
    event_type TEXT NOT NULL,
    payload JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
