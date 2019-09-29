package main

import (
    "fmt"
    "log"
    "net/http"
    "os"
    "strings"

    "github.com/jzelinskie/geddit"
)

const (
    clientId =     "Kkfhbwt2W5C0Rw"
    clientSecret = "SECRET" // SECRET PASSWORD. DO NOT SHARE PUBLICLY.
    username =     "CrossStitchBot"
    password =     "SECRET" // SECRET PASSWORD. DO NOT SHARE PUBLICLY.
)

func summonContestants(post *geddit.Submission) error {
    log.Printf("Summoning contestants to post %s!", post.Permalink)
    return nil
}

func main() {
    // Authenticate.
    session, err := geddit.NewOAuthSession(
        clientId,
        clientSecret,
        "gedditAgent v1",
        "redirect.url",
    )
    if err != nil {
        log.Fatalf("Failed to create new OAuth session: %s", err)
    }
    if err = session.LoginAuth(username, password); err != nil {
        log.Fatalf("Failed to authenticate: %s", err)
    }

    // Get r/CrossStitch submissions, sorted by new.
    submissions, err := session.SubredditSubmissions("CrossStitch", geddit.NewSubmissions, geddit.ListingOptions{
        Limit: 10,
    })
    if err != nil {
        log.Fatalf("Failed to list recent submissions: %s", err)
    }

    // Check submissions for necessary actions.
    for i, post := range submissions {
        // Check for monthly competition post.
        if strings.HasPrefix(post.Title, "[MOD]") && strings.Contains(post.Title, "competition") {
            if err := summonContestants(post); err != nil {
                log.Fatalf("Failed to summon contestants: %s", err)
            }
        }

        // Add more checks here!
        log.Printf("Checked post %d: %s", i, post.Title)
    }

    log.Print("DONE")



    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintln(w, "hello world")
    })
    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }
    log.Fatal(http.ListenAndServe(":"+port, nil))
}
