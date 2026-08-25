-- +goose Up
-- O／KR 层级（PRD §4.1 核心对象；AC-01 表格式创建）。
-- KR 的「状态」即风险等级（词汇表：风险等级），三值枚举在 domain 层校验，库里存文本。
CREATE TABLE objectives (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id  BIGINT      NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    title       TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    sort_order  INT         NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE key_results (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    objective_id BIGINT      NOT NULL REFERENCES objectives (id) ON DELETE CASCADE,
    description  TEXT        NOT NULL,
    metric       TEXT        NOT NULL DEFAULT '',
    owner_id     BIGINT      REFERENCES users (id),
    start_date   DATE,
    end_date     DATE,
    risk_level   TEXT        NOT NULL DEFAULT 'normal',
    sort_order   INT         NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_objectives_project ON objectives (project_id);
CREATE INDEX idx_key_results_objective ON key_results (objective_id);

-- +goose Down
DROP TABLE key_results;
DROP TABLE objectives;
