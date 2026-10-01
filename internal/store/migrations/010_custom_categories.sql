-- Categories the user created can be moved and deleted, built-in ones only renamed
ALTER TABLE categories ADD COLUMN custom BOOLEAN NOT NULL DEFAULT false;
UPDATE categories SET custom = true WHERE sort >= 500;
