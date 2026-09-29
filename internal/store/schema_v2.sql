-- Cards hold one Markdown body whose first line is the "# <title>" heading.
ALTER TABLE tasks ADD COLUMN body TEXT NOT NULL DEFAULT '';

UPDATE tasks SET body = '# ' || title || CASE WHEN description = '' THEN '' ELSE char(10) || char(10) || description END;

ALTER TABLE tasks DROP COLUMN title;
ALTER TABLE tasks DROP COLUMN description;
