package main

import (
    "context"
    "errors"
    "log"
    "net/http"
    "os"
    "strconv"
    "strings"

    "cloud.google.com/go/datastore"

    "github.com/jzelinskie/geddit"

    "google.golang.org/api/option"
    "google.golang.org/api/sheets/v4"
)

const (
    redditClientId =     "Kkfhbwt2W5C0Rw"
    redditUsername =     "CrossStitchBot"

    googleCloudProjectId =     "crossstitch-bot-1569769426365"
    googleCompetitionSheetId = "1BgsXzNY1L4cevQllAblDgCffO7DGNp0eOW4Bs1qbiMA"
    googleCredentialsFile =    "crossstitch-bot-1569769426365-8302a8ad5d0d.json"
)

// Build the contents of the Reddit comment that will summon challenge subscribers.
func buildSummonString(ctx context.Context, useCreds bool) (string, error) {
    const (
        lastSubmissionIndex = 0 // A
        usernameIndex =       2 // C
        subscribedIndex =     3 // D
    )

    // Create an authenticated Google Sheets service.
    var sheetsService *sheets.Service
    var err error
    if useCreds {
        sheetsService, err = sheets.NewService(ctx,
            option.WithScopes(sheets.SpreadsheetsReadonlyScope),
            option.WithCredentialsFile(googleCredentialsFile),
        )
    } else {
        sheetsService, err = sheets.NewService(ctx, option.WithScopes(sheets.SpreadsheetsReadonlyScope))
    }
    if err != nil {
        log.Printf("Failed to create Google Sheets service: %v", err)
        return "", err
    }

    // Read the range of values from the spreadsheet.
    readRange := "LastSubmit!A2:D"
    resp, err := sheetsService.Spreadsheets.Values.Get(googleCompetitionSheetId, readRange).Do()
    if err != nil {
        log.Printf("Unable to retrieve data from Google Sheet: %v", err)
        return "", err
    }

    // Build the summon string.
    text := "Summoning challenge contestants! Paging "
    for i, row := range resp.Values {
        username := row[usernameIndex].(string)
        if !strings.HasPrefix(username, "u/") {
            // Skip rows with invalid usernames.
            log.Printf("Invalid Reddit username column %d, row %d: '%s'", usernameIndex, i, username)
            continue
        }
        lastSubmission, err := strconv.ParseBool(row[lastSubmissionIndex].(string))
        if err != nil {
            // Skip rows with invalid bools.
            log.Printf("Invalid boolean column %d, row %d: '%s'", lastSubmissionIndex, i, row[lastSubmissionIndex])
            continue
        }
        subscribed, err := strconv.ParseBool(row[subscribedIndex].(string))
        if err != nil {
            // Skip rows with invalid bools.
            log.Printf("Invalid boolean column %d, row %d: '%s'", subscribedIndex, i, row[subscribedIndex])
            continue
        }
        if lastSubmission && subscribed {
            if i != 0 {
                text += ", "
            }
            text = text + username
        }
    }
    return text, nil
}

// Summons contestants to a Reddit competition post.
func summonContestants(session *geddit.OAuthSession, post *geddit.Submission, useCreds bool) error {
    log.Printf("Summoning contestants to post %s!", post.Permalink)
    ctx := context.Background()

    // Create an authenticated Google Cloud Datastore client.
    var dsClient *datastore.Client
    var err error
    if useCreds {
        dsClient, err = datastore.NewClient(ctx,
            googleCloudProjectId,
            option.WithCredentialsFile(googleCredentialsFile),
        )
    } else {
        dsClient, err = datastore.NewClient(ctx, googleCloudProjectId)
    }
    if err != nil {
        log.Printf("Failed to create a new Datastore client: %v", err)
        return err
    }

    // Check if this post has already been handled. If so, we're done!
    postKey := datastore.NameKey("Entity", post.ID, nil)
    e := struct{}{}
    if err := dsClient.Get(ctx, postKey, &e); err != datastore.ErrNoSuchEntity{
        if err == nil {
            log.Print("Post has already been processed!")
            return nil
        }
        log.Printf("Error checking for existence of Datastore entity for post: %v", err)
        return err
    }
    log.Print("Post has not been processed yet!")

    // Build the summon string from Google Sheets data.
    text, err := buildSummonString(ctx, useCreds)
    if err != nil {
        return err
    }

    // Comment on the competition post to summon the subscribed users.
    log.Printf(text)
    _, err = session.Reply(post, text)
    if err != nil {
        return err
    }

    // Record handling of this post.
    if _, err := dsClient.Put(ctx, postKey, &e); err != nil {
        log.Printf("Failed to create Datastore entity to record handling of post: %v", err)
        return err
    }
    return nil
}

// Fetches recent Reddit posts and acts on them as necessary.
func checkPosts(useCreds bool) error {
    // Authenticate with Reddit.
    redditClientSecret := os.Getenv("REDDIT_CLIENT_SECRET")
    if redditClientSecret == "" {
        log.Print("REDDIT_CLIENT_SECRET not set")
        return errors.New("REDDIT_CLIENT_SECRET not set")
    }
    session, err := geddit.NewOAuthSession(
        redditClientId,
        redditClientSecret,
        "gedditAgent v1",
        "redirect.url",
    )
    if err != nil {
        log.Printf("Failed to create new Reddit OAuth session: %v", err)
        return err
    }
    redditPassword := os.Getenv("REDDIT_PASSWORD")
    if redditPassword == "" {
        log.Print("REDDIT_PASSWORD not set")
        return errors.New("REDDIT_PASSWORD not set")
    }
    if err = session.LoginAuth(redditUsername, redditPassword); err != nil {
        log.Printf("Failed to authenticate with Reddit: %v", err)
        return err
    }

    // Get r/CrossStitch submissions, sorted by new.
    submissions, err := session.SubredditSubmissions("CrossStitch", geddit.NewSubmissions, geddit.ListingOptions{
        Limit: 20,
    })
    if err != nil {
        log.Printf("Failed to list recent subreddit submissions: %v", err)
        return err
    }

    // Check submissions for necessary actions.
    for _, post := range submissions {
        // Check for monthly competition post.
        if strings.HasPrefix(post.Title, "[MOD]") && strings.Contains(post.Title, "competition") && !strings.Contains(post.Title, "winner") {
            if err := summonContestants(session, post, useCreds); err != nil {
                log.Printf("Failed to summon contestants to post %s: %v", post.Permalink, err)
                return err
            }
        }

        // Add more checks here!
    }

    log.Print("DONE")
    return nil
}

// HttpInvoke is the method that is invoked in Cloud Functions when an HTTP request is received.
func HttpInvoke(http.ResponseWriter, *http.Request) {
    if err := checkPosts(false); err != nil {
        log.Fatalf("Failed to process posts: %v", err)
    }
}

// main is the method that is invoked when running the program locally.
func main() {
    if err := checkPosts(true); err != nil {
        log.Fatalf("Failed to process posts: %v", err)
    }
}
