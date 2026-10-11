CREATE UNIQUE INDEX categories_name_casefold_idx ON categories(lower(name));
CREATE UNIQUE INDEX projects_name_casefold_idx ON projects(lower(name));
CREATE UNIQUE INDEX project_links_project_url_idx ON project_links(project_id, url);
