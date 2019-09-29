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
func summonContestants(post *geddit.Submission) error {
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
    readRange := "Sheet1!A1:B"
    resp, err := sheetsService.Spreadsheets.Values.Get(googleSheetId, readRange).Do()
    if err != nil {
        log.Fatalf("Unable to retrieve data from Google Sheet: %v", err)
    }

    text := "Summoning challenge contestants! Paging "
    for i, row := range resp.Values {
        // Print columns A and B, which correspond to indices 0 and 1.
        username := row[0].(string)
        subscribed, err := strconv.ParseBool(row[1].(string))
        if err != nil {
            log.Fatalf("Invalid Google Sheet data; invalid boolean row %d, column 1: %v", i, row[1])
        }
        if subscribed {
            if i != 0 {
                text += ", "
            }
            text = text + username
        }
    }
    log.Printf(text)
    return nil
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
    for i, post := range submissions {
        // Check for monthly competition post.
        if strings.HasPrefix(post.Title, "[MOD]") && strings.Contains(post.Title, "competition") {
            if err := summonContestants(post); err != nil {
                log.Fatalf("Failed to summon contestants to post %s: %v", post.Permalink, err)
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
