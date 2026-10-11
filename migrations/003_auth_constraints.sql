CREATE UNIQUE INDEX users_username_casefold_idx ON users(lower(username));
