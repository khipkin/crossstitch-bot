package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/api/sheets/v4"

	"cloud.google.com/go/datastore"
	"github.com/khipkin/geddit"
)

type fakeRedditSession struct {
	numComments int
	submittions []*geddit.Submission
}

func (frs *fakeRedditSession) LoginAuth(username, password string) error { return nil }
func (frs *fakeRedditSession) Reply(r geddit.Replier, comment string) (*geddit.Comment, error) {
	frs.numComments++
	return nil, nil
}
func (frs *fakeRedditSession) SubredditSubmissions(subreddit string, sort geddit.PopularitySort, params geddit.ListingOptions) ([]*geddit.Submission, error) {
	return frs.submittions, nil
}
func (frs *fakeRedditSession) Throttle(interval time.Duration) {}

type fakeDatastoreClient struct {
	lastPut map[string]interface{}
}

func (fdc *fakeDatastoreClient) Get(ctx context.Context, key *datastore.Key, dst interface{}) error {
	dst, ok := fdc.lastPut[fmt.Sprintf("%s-%s", key.Kind, key.Name)]
	if !ok {
		return datastore.ErrNoSuchEntity
	}
	return nil
}
func (fdc *fakeDatastoreClient) Put(ctx context.Context, key *datastore.Key, src interface{}) (*datastore.Key, error) {
	fdc.lastPut[fmt.Sprintf("%s-%s", key.Kind, key.Name)] = src
	return key, nil
}

func fakeSummoner(redditSubmissions []*geddit.Submission, spreadsheetValues *sheets.ValueRange) *summoner {
	return &summoner{
		redditSession:             &fakeRedditSession{submittions: redditSubmissions},
		datastoreClient:           &fakeDatastoreClient{lastPut: map[string]interface{}{}},
		readSpreadsheetValuesFunc: func(string, string) (*sheets.ValueRange, error) { return spreadsheetValues, nil },
	}
}

func generateFakeUsers(numUsers int) [][]interface{} {
	values := make([][]interface{}, numUsers)
	for i := 0; i < numUsers; i++ {
		v := make([]interface{}, 1)
		v[0] = fmt.Sprintf("u/user-%d", i)
		values[i] = v
	}
	return values
}

func TestBuildSummonStringsNoValues(t *testing.T) {
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: [][]interface{}{}})
	summons, err := s.buildSummonStrings()
	if err != nil {
		t.Fatalf("buildSummonStrings call failed: %v", err)
	}
	if len(summons) != 0 {
		t.Fatalf("buildSummonStrings returned non-empty results: %s", summons)
	}
}

func TestBuildSummonStringsSomeValuesWithRemainder(t *testing.T) {
	const numUsers = maxRedditTagsPerComment + 1
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})
	summons, err := s.buildSummonStrings()
	if err != nil {
		t.Fatalf("buildSummonStrings call failed: %v", err)
	}
	if len(summons) != 2 {
		t.Fatalf("buildSummonStrings returned results of wrong length: %s", summons)
	}
}

func TestBuildSummonStringsSomeValuesNoRemainder(t *testing.T) {
	const numUsers = maxRedditTagsPerComment
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})
	summons, err := s.buildSummonStrings()
	if err != nil {
		t.Fatalf("buildSummonStrings call failed: %v", err)
	}
	if len(summons) != 1 {
		t.Fatalf("buildSummonStrings returned results of wrong length: %s", summons)
	}
}

func TestSummonContestantsNoUsers(t *testing.T) {
	post := &geddit.Submission{FullID: "t3_12345"}
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: [][]interface{}{}})

	if err := s.summonContestants(context.Background(), post); err != nil {
		t.Fatalf("summonContestants call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != 0 {
		t.Fatalf("summonContestants made unexpected number of comments (got: %d, want: %d)", fsr.numComments, 0)
	}
}

func TestSummonContestantsSomeUsers(t *testing.T) {
	const numUsers = maxRedditTagsPerComment + 1
	const expectedNumComments = 3 // main comment, 2 child comments
	post := &geddit.Submission{FullID: "t3_12345"}
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})

	if err := s.summonContestants(context.Background(), post); err != nil {
		t.Fatalf("summonContestants call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != expectedNumComments {
		t.Fatalf("summonContestants made unexpected number of comments (got: %d, want: %d)", fsr.numComments, expectedNumComments)
	}
}

func TestHandlePossibleCompetitionPostWinnersPost(t *testing.T) {
	const numUsers = maxRedditTagsPerComment + 1
	post := &geddit.Submission{FullID: "t3_12345", Title: "[MOD] January's competition winners - more text"}
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})

	if err := s.handlePossibleCompetitionPost(context.Background(), post); err != nil {
		t.Fatalf("handlePossibleCompetitionPost call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != 0 {
		t.Fatalf("handlePossibleCompetitionPost made unexpected number of comments (got: %d, want: %d)", fsr.numComments, 0)
	}
}

func TestHandlePossibleCompetitionPostNotHandledYet(t *testing.T) {
	const numUsers = maxRedditTagsPerComment + 1
	const expectedNumComments = 3 // main comment, 2 child comments
	post := &geddit.Submission{FullID: "t3_12345", Title: "[MOD] January's competition - more text"}
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})

	if err := s.handlePossibleCompetitionPost(context.Background(), post); err != nil {
		t.Fatalf("handlePossibleCompetitionPost call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != expectedNumComments {
		t.Fatalf("handlePossibleCompetitionPost made unexpected number of comments (got: %d, want: %d)", fsr.numComments, expectedNumComments)
	}
}

func TestHandlePossibleCompetitionPostAlreadyHandled(t *testing.T) {
	const numUsers = maxRedditTagsPerComment + 1
	ctx := context.Background()
	post := &geddit.Submission{FullID: "t3_12345", Title: "[MOD] January's competition - more text"}
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})
	val := struct{}{}
	if _, err := s.datastoreClient.Put(ctx, datastore.NameKey("Entity", post.FullID, nil), &val); err != nil {
		t.Fatalf("test failed to setup datastore state: %v", err)
	}

	if err := s.handlePossibleCompetitionPost(ctx, post); err != nil {
		t.Fatalf("handlePossibleCompetitionPost call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != 0 {
		t.Fatalf("handlePossibleCompetitionPost made unexpected number of comments (got: %d, want: %d)", fsr.numComments, 0)
	}
}

func TestCheckPostsHandlesSeveralPosts(t *testing.T) {
	const numUsers = maxRedditTagsPerComment + 1
	const expectedNumComments = 6 // 2 * (main comment, 2 child comments)
	submissions := []*geddit.Submission{
		&geddit.Submission{FullID: "t3_12345", Title: "[MOD] January's competition - more text"},
		&geddit.Submission{FullID: "t3_67890", Title: "[MOD] February's competition - more text"},
	}
	s := fakeSummoner(submissions, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})

	if err := s.checkPosts(context.Background()); err != nil {
		t.Fatalf("checkPosts call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != expectedNumComments {
		t.Fatalf("checkPosts made unexpected number of comments (got: %d, want: %d)", fsr.numComments, expectedNumComments)
	}
}
