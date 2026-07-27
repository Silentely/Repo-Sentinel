env "sqlite" {
  url = getenv("REPOSENTINEL_ATLAS_SQLITE_URL")
  dev = "sqlite://file?mode=memory&_fk=1"
  migration {
    dir = "file://migrations/sqlite"
  }
}

env "postgres" {
  url = getenv("REPOSENTINEL_ATLAS_POSTGRES_URL")
  dev = "docker://postgres/17/dev?search_path=public"
  migration {
    dir = "file://migrations/postgres"
  }
}
