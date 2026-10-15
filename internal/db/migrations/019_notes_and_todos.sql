-- A task is a short text plus todos, its steps. Longer work (research, shortlists, drafts) lives in
-- notes: markdown documents inside a project, optionally written for one task.
CREATE TABLE task_todos (
    id         uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    task_id    text NOT NULL,
    position   int  NOT NULL DEFAULT 0,
    text       text NOT NULL,
    done       bool NOT NULL DEFAULT false,
    done_by    uuid,
    done_at    timestamptz,
    -- result is one line: what the agent found for this step.
    result     text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, task_id) REFERENCES tasks(org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, done_by) REFERENCES members(org_id, id)
);
CREATE INDEX task_todos_task ON task_todos (org_id, task_id, position);

CREATE TABLE notes (
    org_id       uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    id           text NOT NULL,
    project_id   uuid NOT NULL,
    -- task_id is the task the note was written for, if any.
    task_id      text,
    title        text NOT NULL,
    body         text NOT NULL DEFAULT '',
    created_by   uuid NOT NULL,
    on_behalf_of uuid,
    updated_by   uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz,
    deleted_by   uuid,
    PRIMARY KEY (org_id, id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id),
    FOREIGN KEY (org_id, task_id) REFERENCES tasks(org_id, id) ON DELETE SET NULL (task_id),
    FOREIGN KEY (org_id, created_by) REFERENCES members(org_id, id),
    FOREIGN KEY (org_id, on_behalf_of) REFERENCES members(org_id, id),
    FOREIGN KEY (org_id, updated_by) REFERENCES members(org_id, id),
    FOREIGN KEY (org_id, deleted_by) REFERENCES members(org_id, id)
);
CREATE INDEX notes_project ON notes (org_id, project_id, updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX notes_task ON notes (org_id, task_id) WHERE task_id IS NOT NULL;

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['task_todos', 'notes']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('CREATE POLICY org_isolation ON %I
            USING (org_id = nullif(current_setting(''app.org_id'', true), '''')::uuid)
            WITH CHECK (org_id = nullif(current_setting(''app.org_id'', true), '''')::uuid)', t);
    END LOOP;
END $$;

-- Note events go in events like any other, with data {note_id, title, project_id} and task_id set to
-- the note's task when it has one. Open screens hear them by the note's project.
CREATE INDEX events_note ON events (org_id, (data->>'note_id'), id) WHERE (data->>'note_id') IS NOT NULL;

CREATE OR REPLACE FUNCTION notify_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('handoff_live', json_build_object('org', NEW.org_id, 'kind', 'event', 'task', NEW.task_id,
        'actor', NEW.actor_id,
        'project', CASE WHEN NEW.data->>'note_id' IS NOT NULL THEN NEW.data->>'project_id'
            ELSE (SELECT project_id::text FROM tasks WHERE org_id = NEW.org_id AND id = NEW.task_id) END,
        'from_project', NEW.data->>'from_project_id')::text);
    RETURN NULL;
END $$;

-- checklist_split takes a markdown checklist out of a task body: each "- [ ] step" line (any list
-- marker, outside code fences) becomes a todo, in order, and the lines indented under an item become
-- its result, joined into one line. rest is everything else, the task's plain text.
CREATE FUNCTION checklist_split(body text, OUT todos jsonb, OUT rest text) LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    line     text;
    m        text[];
    in_fence bool := false;
    cur      int := -1;  -- index of the todo whose sub-lines we're reading, or -1
    cur_ind  int := 0;
    kept     text[] := '{}';
    sub      text;
BEGIN
    todos := '[]';
    FOREACH line IN ARRAY string_to_array(coalesce(body, ''), E'\n')
    LOOP
        IF line ~ '^\s*(```|~~~)' THEN
            in_fence := NOT in_fence;
            cur := -1;
            kept := kept || line;
            CONTINUE;
        END IF;
        IF in_fence THEN
            kept := kept || line;
            CONTINUE;
        END IF;
        m := regexp_match(line, '^(\s*)(?:[-*+]|\d+[.)])\s+\[([ xX])\]\s*(.*)$');
        IF m IS NOT NULL THEN
            IF btrim(m[3]) = '' THEN
                cur := -1;
                CONTINUE; -- an empty box says nothing
            END IF;
            todos := todos || jsonb_build_array(jsonb_build_object('text', left(btrim(m[3]), 300), 'done', m[2] <> ' ', 'result', ''));
            cur := jsonb_array_length(todos) - 1;
            cur_ind := length(m[1]);
            CONTINUE;
        END IF;
        IF cur >= 0 AND line ~ '^\s+\S' AND length(substring(line FROM '^\s*')) > cur_ind THEN
            sub := btrim(regexp_replace(line, '^\s*(?:(?:[-*+]|\d+[.)])\s+)?', ''));
            IF sub <> '' THEN
                todos := jsonb_set(todos, ARRAY[cur::text, 'result'], to_jsonb(left(concat_ws('; ', nullif(todos->cur->>'result', ''), sub), 500)));
            END IF;
            CONTINUE;
        END IF;
        IF btrim(line) <> '' THEN
            cur := -1;
        END IF;
        kept := kept || line;
    END LOOP;
    rest := btrim(regexp_replace(array_to_string(kept, E'\n'), E'\n{3,}', E'\n\n', 'g'), E' \t\n');
END $$;

-- Existing tasks are converted in place: their checklists become todos, the rest stays their text.
CREATE TEMP TABLE converted ON COMMIT DROP AS
    SELECT t.org_id, t.id, s.todos, s.rest FROM tasks t, checklist_split(t.body) s
    WHERE t.body ~ '\[[ xX]\]' AND jsonb_array_length(s.todos) > 0;

INSERT INTO task_todos (org_id, task_id, position, text, done, done_at, result)
SELECT c.org_id, c.id, (j.n - 1)::int, j.v->>'text', (j.v->>'done')::bool,
    CASE WHEN (j.v->>'done')::bool THEN now() END, j.v->>'result'
FROM converted c, jsonb_array_elements(c.todos) WITH ORDINALITY AS j(v, n);

UPDATE tasks t SET body = c.rest FROM converted c WHERE t.org_id = c.org_id AND t.id = c.id;
