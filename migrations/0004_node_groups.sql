CREATE TABLE node_groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE node_group_memberships (
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (node_id, group_id)
);

CREATE TABLE member_node_group_grants (
    group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    member_id TEXT NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, member_id)
);

CREATE INDEX idx_node_group_memberships_group_id ON node_group_memberships(group_id);
CREATE INDEX idx_member_node_group_grants_member_id ON member_node_group_grants(member_id);
