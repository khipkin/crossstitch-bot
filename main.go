package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "os"
    "strconv"
    "strings"

    "github.com/jzelinskie/geddit"

    "google.golang.org/api/option"
    "google.golang.org/api/sheets/v4"
)

const (
    redditClientId =     "Kkfhbwt2W5C0Rw"
    redditClientSecret = "SECRET" // SECRET PASSWORD. DO NOT SHARE PUBLICLY.
    redditUsername =     "CrossStitchBot"
    redditPassword =     "SECRET" // SECRET PASSWORD. DO NOT SHARE PUBLICLY.

    googleSheetId = "1sU7OwYp9kjF0vD1uXIactca6Z_RuD6AYh7g6O5v_CPM"
)

// Summons contestants to a reddit competition post.
func summonContestants(session *geddit.OAuthSession, post *geddit.Submission) error {
    log.Printf("Summoning contestants to post %s!", post.Permalink)

    // Create an authenticated client.
    ctx := context.Background()
    sheetsService, err := sheets.NewService(ctx,
        option.WithScopes(sheets.SpreadsheetsReadonlyScope),
        option.WithCredentialsFile("crossstitch-bot-1569769426365-1bf5b821811c.json"),
    )
    if err != nil {
        log.Fatalf("Failed to create Google Sheets service: %v", err)
    }

    // Read and print the usernames from the spreadsheet.
    const (
        usernameIndex =   0 // A
        subscribedIndex = 1 // B
    )
    readRange := "Sheet1!A1:B"
    resp, err := sheetsService.Spreadsheets.Values.Get(googleSheetId, readRange).Do()
    if err != nil {
        log.Fatalf("Unable to retrieve data from Google Sheet: %v", err)
    }

    text := "Summoning challenge contestants! Paging "
    for i, row := range resp.Values {
        username := row[usernameIndex].(string)
        if !strings.HasPrefix(username, "u/") {
            // Skip rows with invalid usernames.
            log.Printf("Invalid Reddit username column %d, row %d: '%s'", usernameIndex, i, username)
            continue
        }
        subscribed, err := strconv.ParseBool(row[subscribedIndex].(string))
        if err != nil {
            // Skip rows with invalid bools.
            log.Printf("Invalid boolean column %d, row %d: '%s'", subscribedIndex, i, row[subscribedIndex])
            continue
        }
        if subscribed {
            if i != 0 {
                text += ", "
            }
            text = text + username
        }
    }
    log.Printf(text)
    _, err = session.Reply(post, text)
    return err
}

func main() {
    // Authenticate with Reddit.
    session, err := geddit.NewOAuthSession(
        redditClientId,
        redditClientSecret,
        "gedditAgent v1",
        "redirect.url",
    )
    if err != nil {
        log.Fatalf("Failed to create new Reddit OAuth session: %v", err)
    }
    if err = session.LoginAuth(redditUsername, redditPassword); err != nil {
        log.Fatalf("Failed to authenticate with Reddit: %v", err)
    }

    // Get r/CrossStitch submissions, sorted by new.
    submissions, err := session.SubredditSubmissions("CrossStitch", geddit.NewSubmissions, geddit.ListingOptions{
        Limit: 10,
    })
    if err != nil {
        log.Fatalf("Failed to list recent subreddit submissions: %v", err)
    }

    // Check submissions for necessary actions.
    for _, post := range submissions {
        // Check for monthly competition post.
        if strings.HasPrefix(post.Title, "[MOD]") && strings.Contains(post.Title, "competition") {
            if err := summonContestants(session, post); err != nil {
                log.Fatalf("Failed to summon contestants to post %s: %v", post.Permalink, err)
            }
        }

        // Add more checks here!
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
