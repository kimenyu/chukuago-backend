-- Migration: 005_chat.up.sql
-- One conversation per errand, many messages per conversation.

CREATE TYPE message_type AS ENUM ('text', 'image', 'file', 'system');

CREATE TABLE conversations (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id  UUID        NOT NULL UNIQUE REFERENCES errands (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_conversations_errand_id ON conversations (errand_id);

CREATE TABLE messages (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID         NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    sender_id       UUID         NOT NULL REFERENCES users (id),
    message_type    message_type NOT NULL DEFAULT 'text',
    content         TEXT,
    attachment_url  TEXT,
    metadata        JSONB        NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    read_at         TIMESTAMPTZ,

    CONSTRAINT message_has_content CHECK (
        content IS NOT NULL OR attachment_url IS NOT NULL
    )
);

CREATE INDEX idx_messages_conversation_id ON messages (conversation_id);
CREATE INDEX idx_messages_sender_id       ON messages (sender_id);
CREATE INDEX idx_messages_created         ON messages (conversation_id, created_at ASC);
