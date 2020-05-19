package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/datastore"

	"github.com/khipkin/geddit"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

const (
	subreddit      = "CrossStitch"
	redditClientID = "Kkfhbwt2W5C0Rw"
	redditUsername = "CrossStitchBot"

	googleCloudProjectID     = "crossstitch-bot-1569769426365"
	googleCompetitionSheetID = "1BgsXzNY1L4cevQllAblDgCffO7DGNp0eOW4Bs1qbiMA"
	googleCredentialsFile    = "crossstitch-bot-1569769426365-7aaa0bc7606d.json"

	maxUsersPerSession      = 12
	maxRedditTagsPerComment = 3
)

// PageToken for processing and tagging users on a competition post.
type PageToken struct {
	MainCommentFullID string
	LastProcessedUser string
}

type oAuthSession interface {
	LoginAuth(username, password string) error
	Reply(r geddit.Replier, comment string) (*geddit.Comment, error)
	SubredditSubmissions(subreddit string, sort geddit.PopularitySort, params geddit.ListingOptions) ([]*geddit.Submission, error)
	Throttle(interval time.Duration)
	Comment(subreddit, fullID string) (*geddit.Comment, error)
}

type datastoreClient interface {
	Get(ctx context.Context, key *datastore.Key, dst interface{}) error
	GetAll(ctx context.Context, q *datastore.Query, dst interface{}) (keys []*datastore.Key, err error)
	Put(ctx context.Context, key *datastore.Key, src interface{}) (*datastore.Key, error)
	Delete(ctx context.Context, key *datastore.Key) error
}

type summoner struct {
	redditSession             oAuthSession
	datastoreClient           datastoreClient
	sheetsService             *sheets.Service
	readSpreadsheetValuesFunc func(string, string) (*sheets.ValueRange, error)
}

func (s *summoner) readSpreadsheetRange(spreadsheetID, readRange string) (*sheets.ValueRange, error) {
	return s.sheetsService.Spreadsheets.Values.Get(spreadsheetID, readRange).Do()
}

func newSummoner(redditSession *geddit.OAuthSession, datastoreClient *datastore.Client, sheetsService *sheets.Service) *summoner {
	s := &summoner{
		redditSession:   redditSession,
		datastoreClient: datastoreClient,
		sheetsService:   sheetsService,
	}
	s.readSpreadsheetValuesFunc = s.readSpreadsheetRange
	return s
}

// Build the contents of the Reddit comment that will summon challenge subscribers.
// Return the list of comment strings and the last username processed, or else an error.
func (s *summoner) buildSummonStrings(lastProccessedUser string) ([]string, string, error) {
	const usernameIndex = 0 // Column A

	// Read the range of values from the spreadsheet.
	readRange := "SignedUp!A2:A"
	resp, err := s.readSpreadsheetValuesFunc(googleCompetitionSheetID, readRange)
	if err != nil {
		log.Printf("Unable to retrieve data from Google Sheets: %v", err)
		return nil, "", err
	}

	// If we are starting from a specific last processed user, first find that user's index.
	firstIndexToProcess := 0
	if lastProccessedUser != "" {
		for i, row := range resp.Values {
			if lastProccessedUser == row[usernameIndex].(string) {
				firstIndexToProcess = i + 1
				break
			}
		}
	}

	// Build the summon strings, starting from user after the last processed user, up to the max number of users per session.
	var usersToProcess = firstIndexToProcess + maxUsersPerSession
	if len(resp.Values) < usersToProcess {
		usersToProcess = len(resp.Values)
	}
	var summons = []string{}
	var curr = ""
	var seen = 0
	var username = ""
	for i := firstIndexToProcess; i < usersToProcess; i++ {
		// Process the values from the spreadsheet row.
		row := resp.Values[i]
		username = row[usernameIndex].(string)
		if !strings.HasPrefix(username, "u/") {
			// Skip rows with invalid usernames.
			log.Printf("Invalid Reddit username column %d, row %d: '%s'", usernameIndex, i, username)
			continue
		}

		// Build the summon string.
		if seen%maxRedditTagsPerComment == 0 {
			curr = "Summoning contestants "
		} else {
			curr += ", "
		}
		curr = curr + username
		seen = seen + 1
		if seen%maxRedditTagsPerComment == 0 && seen > 0 {
			summons = append(summons, curr)
			seen = 0
		}
	}
	if seen > 0 {
		summons = append(summons, curr)
	}
	// If we finished processing returned users, return empty last user.
	if len(resp.Values) == usersToProcess {
		username = ""
	}
	return summons, username, nil
}

// Summons contestants to a Reddit competition post.
func (s *summoner) summonContestants(ctx context.Context, post *geddit.Submission, pageToken *PageToken) error {
	if pageToken != nil {
		log.Printf("Summoning contestants under comment '%s' starting with %s!", pageToken.MainCommentFullID, pageToken.LastProcessedUser)
	} else {
		log.Printf("Summoning contestants to post '%s'!", post.FullID)
	}

	lpu := ""
	if pageToken != nil {
		lpu = pageToken.LastProcessedUser
	}
	// Build the summon string from Google Sheets data. If there are no subscribed users, we're done.
	summons, lastUser, err := s.buildSummonStrings(lpu)
	if err != nil {
		return err
	}
	if len(summons) == 0 {
		return nil
	}

	// If this is the first time processing this post, make the parent comment. Otherwise get the comment from the PageToken.
	var mainCommentFullID = ""
	var mainComment *geddit.Comment
	if pageToken == nil {
		// Make the main comment on which all users will be summoned.
		mainCommentText := "This month's competition is live! Please submit your piece and/or vote for your favorite entries!\n\n" +
			"To subscribe to future monthly competition posts, please fill out [this form](https://forms.gle/4seHL2YRRGTnT96E6)" +
			" and our friendly robot will summon you. You may unsubscribe at any time using the same form!"
		log.Print(mainCommentText)
		var err error
		mainComment, err = s.redditSession.Reply(post, mainCommentText)
		if err != nil {
			log.Printf("Failed to make parent Reddit comment on competition post: %v", err)
			return err
		}
		mainCommentFullID = mainComment.FullID
	} else {
		mainCommentFullID = pageToken.MainCommentFullID
	}

	// If we don't already have it, get the main comment from Reddit so we can make child comments.
	if mainComment == nil {
		mainComment, err = s.redditSession.Comment(subreddit, mainCommentFullID)
		if err != nil {
			log.Printf("Failed to fetch main comment from Reddit: %v", err)
			return err
		}
	}

	ptKey := datastore.NameKey("PageToken", post.FullID, nil)
	if lastUser != "" {
		// If not all users can be processed, write or update the PageToken to Datastore.
		pt := &PageToken{
			MainCommentFullID: mainCommentFullID,
			LastProcessedUser: lastUser,
		}
		log.Printf("Saving PageToken with main comment id %s and last user %s", pt.MainCommentFullID, pt.LastProcessedUser)
		if _, err := s.datastoreClient.Put(ctx, ptKey, pt); err != nil {
			log.Printf("Failed to post PageToken to Datastore: %v", err)
			return err
		}
	} else {
		// If all users have been processed, delete the PageToken from Datastore.
		if err := s.datastoreClient.Delete(ctx, ptKey); err != nil {
			if err != datastore.ErrNoSuchEntity {
				log.Printf("Failed to delete PageToken from Datastore: %v", err)
				return err
			}
		}
	}

	// Make the child comments on the original Reddit comment.
	for _, summonText := range summons {
		log.Printf("\t%s", summonText)
		_, err = s.redditSession.Reply(mainComment, summonText)
		if err != nil {
			log.Printf("Failed to make child Reddit comment on competition post: %v", err)
			continue
		}
	}
	return nil
}

func (s *summoner) handlePossibleCompetitionPost(ctx context.Context, post *geddit.Submission) error {
	if strings.HasPrefix(post.Title, "[MOD]") && strings.Contains(post.Title, "competition") && !strings.Contains(post.Title, "winner") {
		// Check if this post is already in progress. If so, continue where we left off.
		postKey := datastore.NameKey("PageToken", post.FullID, nil)
		pt := PageToken{}
		if err := s.datastoreClient.Get(ctx, postKey, &pt); err == nil {
			log.Printf("Competition post processing in progress! Continuing with user %s!", pt.LastProcessedUser)
			if err := s.summonContestants(ctx, post, &pt); err != nil {
				log.Printf("Failed to continue summoning contestants to post %s: %v", post.FullID, err)
				return err
			}
			return nil
		}

		// If the post is not in progress, check if this post has already been handled. If so, we're done!
		postKey = datastore.NameKey("Entity", post.FullID, nil)
		e := struct{}{}
		if err := s.datastoreClient.Get(ctx, postKey, &e); err != datastore.ErrNoSuchEntity {
			if err == nil {
				log.Print("Post has already been processed!")
				return nil
			}
			log.Printf("Error checking for existence of Datastore entity for post: %v", err)
			return err
		}
		log.Print("Competition post has not been processed yet!")

		// Record handling of this post. This must be done before the actual handling, otherwise
		// posts will be handled again if the function times out.
		if _, err := s.datastoreClient.Put(ctx, postKey, &e); err != nil {
			log.Printf("Failed to create Datastore entity to record handling of post: %v", err)
			return err
		}

		// Handle the post.
		if err := s.summonContestants(ctx, post, nil /*PageToken*/); err != nil {
			log.Printf("Failed to summon contestants to post %s: %v", post.FullID, err)
			return err
		}
	}
	return nil
}

func (s *summoner) handlePageToken(ctx context.Context, postID string, pt *PageToken) error {
	log.Printf("Page token handling in progress! Continuing with user %s!", pt.LastProcessedUser)

	// Get the reddit post
	if err := s.summonContestants(ctx, &geddit.Submission{FullID: postID}, pt); err != nil {
		log.Printf("Failed to continue summoning contestants to post '%s': %v", postID, err)
		return err
	}
	return nil
}

// Fetches recent Reddit posts and acts on them as necessary.
func (s *summoner) checkPosts(ctx context.Context) error {
	// Get submissions from the subreddit, sorted by new, and process them.
	submissions, err := s.redditSession.SubredditSubmissions(subreddit, geddit.NewSubmissions, geddit.ListingOptions{
		Limit: 20,
	})
	if err != nil {
		log.Printf("Failed to list recent subreddit submissions: %v", err)
		return err
	}
	for _, post := range submissions {
		// Check for monthly competition post.
		if err := s.handlePossibleCompetitionPost(ctx, post); err != nil {
			return err
		}

		// Add more checks here!
	}

	// Get PageToken entities from Datastore, and process them.
	tokens := []*PageToken{}
	keys, err := s.datastoreClient.GetAll(ctx, datastore.NewQuery("PageToken"), &tokens)
	if err != nil {
		log.Printf("Failed to list unresolved PageToken entities from Datastore: %v", err)
	}
	for i, key := range keys {
		if err := s.handlePageToken(ctx, key.Name, tokens[i]); err != nil {
			return err
		}
	}

	log.Print("DONE")
	return nil
}

func setupSummoner(ctx context.Context, useCreds bool) (*summoner, error) {
	// Authenticate with Reddit.
	redditClientSecret := os.Getenv("REDDIT_CLIENT_SECRET")
	if redditClientSecret == "" {
		log.Print("REDDIT_CLIENT_SECRET not set")
		return nil, errors.New("REDDIT_CLIENT_SECRET not set")
	}
	redditSession, err := geddit.NewOAuthSession(
		redditClientID,
		redditClientSecret,
		"gedditAgent v1 fork by khipkin",
		"redirect.url",
	)
	if err != nil {
		log.Printf("Failed to create new Reddit OAuth session: %v", err)
		return nil, err
	}
	redditPassword := os.Getenv("REDDIT_PASSWORD")
	if redditPassword == "" {
		log.Print("REDDIT_PASSWORD not set")
		return nil, errors.New("REDDIT_PASSWORD not set")
	}
	if err = redditSession.LoginAuth(redditUsername, redditPassword); err != nil {
		log.Printf("Failed to authenticate with Reddit: %v", err)
		return nil, err
	}

	// To prevent Reddit rate limiting errors, throttle requests.
	redditSession.Throttle(5 * time.Second)

	// Create an authenticated Google Cloud Datastore client.
	var dsClient *datastore.Client
	if useCreds {
		dsClient, err = datastore.NewClient(ctx,
			googleCloudProjectID,
			option.WithCredentialsFile(googleCredentialsFile),
		)
	} else {
		dsClient, err = datastore.NewClient(ctx, googleCloudProjectID)
	}
	if err != nil {
		log.Printf("Failed to create a new Datastore client: %v", err)
		return nil, err
	}

	// Create an authenticated Google Sheets service.
	var sheetsService *sheets.Service
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
		return nil, err
	}

	return newSummoner(redditSession, dsClient, sheetsService), nil
}

// HttpInvoke is the method that is invoked in Google Cloud Functions when an HTTP request is received.
func HttpInvoke(http.ResponseWriter, *http.Request) {
	ctx := context.Background()
	s, err := setupSummoner(ctx, false)
	if err != nil {
		log.Fatalf("Failed to setup summoner: %v", err)
	}
	if err := s.checkPosts(ctx); err != nil {
		log.Fatalf("Failed to process posts: %v", err)
	}
}

// main is the method that is invoked when running the program locally.
func main() {
	ctx := context.Background()
	s, err := setupSummoner(ctx, true)
	if err != nil {
		log.Fatalf("Failed to setup summoner: %v", err)
	}
	if err := s.checkPosts(ctx); err != nil {
		log.Fatalf("Failed to process posts: %v", err)
	}
}
