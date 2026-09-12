-- +goose Up
CREATE TABLE account_recovery_requests (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    matched_student_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    old_telegram_id BIGINT NOT NULL,
    new_telegram_id BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected')),
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reviewed_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX uq_account_recovery_pending_new_tg
    ON account_recovery_requests(new_telegram_id)
    WHERE status = 'pending';

CREATE UNIQUE INDEX uq_account_recovery_pending_user
    ON account_recovery_requests(user_id)
    WHERE status = 'pending';

CREATE INDEX idx_account_recovery_status_requested
    ON account_recovery_requests(status, requested_at);

-- +goose Down
DROP TABLE IF EXISTS account_recovery_requests;
