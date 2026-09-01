// Runs once, on the first boot of a fresh Mongo data volume. Creates the
// application user scoped to the ps188 database (the root user stays for admin).
db = db.getSiblingDB("ps188");

db.createUser({
  user: "ps188_app",
  pwd: "ps188_app",
  roles: [{ role: "readWrite", db: "ps188" }],
});

// Touch a collection so the database is persisted even before the app writes.
db.createCollection("_bootstrap");
